package sshwizard

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/remoteaccess"
)

// THE FIX HALF. Check only reads. Fix does steps 5, 6 and 7 for a step that says todo, and nothing else:
//
//	5  make an ed25519 key with no passphrase, because an agent cannot type one, or read the public half back out of a
//	   private key that has lost its .pub
//	6  publish this room's login user, key, port and switch to the hub
//	7  rewrite atrium's block of the login user's authorized keys to the set the hub relays
//
// A step that says needs-human or failed is left alone and the report says so. Fix never touches sshd, the ssh config
// or the room's switch. It saves the login user when --user named one, because that is the room's own say and the
// hub is told it in step 6.
//
// ATRIUM'S LINES ARE THE ONLY ONES TOUCHED. Step 7 owns the text between `# BEGIN atrium` and `# END atrium` in the
// authorized keys file, and every other line, a person's own keys included, is written back byte for byte. A block with
// no END is refused, because there is no telling where it stops.

// Action is one thing Fix did, or tried to.
type Action struct {
	N    int    `json:"n"`
	Did  string `json:"did"`
	Err  string `json:"error,omitempty"`
	Done bool   `json:"done"`
}

// FixBound is how long the fixes may take together, apart from the checks' own bound.
const FixBound = 60 * time.Second

// Fix runs the checks, does the fixable steps in order, and checks again. It returns the report from after, and what
// it did.
func Fix(ctx context.Context, m Machine, opt Options) (Report, []Action) {
	return fix(ctx, m, opt, []int{5, 6, 7}, true)
}

// Sync is the part of Fix a room can do on its own, on a timer: it never makes a key and never saves a login user.
// steps names which of 6 and 7 to do, so a room with its switch off can publish and leave authorized_keys alone.
func Sync(ctx context.Context, m Machine, opt Options, steps ...int) (Report, []Action) {
	return fix(ctx, m, opt, steps, false)
}

func fix(ctx context.Context, m Machine, opt Options, steps []int, saveUser bool) (Report, []Action) {
	ctx, cancel := context.WithTimeout(ctx, FixBound)
	defer cancel()
	var acts []Action
	if opt.User != "" && saveUser {
		if cur, err := m.Switch(); err == nil && cur.User != opt.User {
			if !remoteaccess.ValidUser(opt.User) {
				acts = append(acts, Action{N: 4, Did: "save " + opt.User + " as the login user", Err: "not a login name"})
			} else if err := m.SaveUser(opt.User); err != nil {
				acts = append(acts, Action{N: 4, Did: "save " + opt.User + " as the login user", Err: err.Error()})
			} else {
				acts = append(acts, Action{N: 4, Did: "saved " + opt.User + " as the login other rooms use", Done: true})
			}
		}
	}
	fixes := map[int]func(context.Context, *Run) (string, error){5: fixKey, 6: fixPublish, 7: fixInstall}
	r, rep := pass(ctx, m, opt)
	for _, n := range steps {
		if rep.Results[n-1].Status != Todo {
			continue
		}
		did, err := fixes[n](ctx, r)
		a := Action{N: n, Did: did, Done: err == nil}
		if err != nil {
			a.Err = err.Error()
		}
		acts = append(acts, a)
		// A pass is only repeated after a fix, so a settled room costs one.
		r, rep = pass(ctx, m, opt)
	}
	return rep, acts
}
func fixKey(ctx context.Context, r *Run) (string, error) {
	priv := keyPathFor(r.GOOS, r.M.Home())
	pub := priv + ".pub"
	if r.M.Exists(priv) {
		// The private half is there and the public half is not: ssh-keygen derives it, and nothing is regenerated.
		out, err := r.M.Run(ctx, "ssh-keygen", "-y", "-f", priv)
		line := strings.TrimSpace(out)
		if err != nil || keyBody(line) == "" {
			return "read the public key out of " + priv, fmt.Errorf("ssh-keygen -y said: %s", strings.TrimSpace(out))
		}
		if err := r.M.WriteFile(pub, []byte(keyBody(line)+" atrium:"+r.Joined.Room+"\n"), 0o644); err != nil {
			return "write " + pub, err
		}
		return "wrote " + pub + " from the private key that was already there", nil
	}
	if err := r.M.MkdirAll(joinFor(r.GOOS, r.M.Home(), ".ssh"), 0o700); err != nil {
		return "make the .ssh directory", err
	}
	out, err := r.M.Run(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "atrium:"+r.Joined.Room, "-f", priv)
	if err != nil {
		return "make " + priv, fmt.Errorf("ssh-keygen said: %s", strings.TrimSpace(out))
	}
	return "made an ed25519 key with no passphrase at " + priv, nil
}

func fixPublish(ctx context.Context, r *Run) (string, error) {
	rec := PublishRecord{User: r.User, Key: r.PubKey, Port: r.port(), On: r.State.On}
	if err := r.M.Publish(ctx, rec); err != nil {
		return "publish this room's key and login to the hub", err
	}
	return fmt.Sprintf("published this room's key, login %s and port %d to the hub", r.User, r.port()), nil
}

func fixInstall(ctx context.Context, r *Run) (string, error) {
	path, admin := AuthorizedFile(ctx, r.M, r.User)
	want, ok := r.wantedKeys(ctx)
	if !ok {
		// Not knowing the hub's set must not strip the keys that are already there.
		return "install the hub's keys", errors.New("the hub's set of keys could not be read")
	}
	cur := ""
	if b, err := r.M.ReadFile(path); err == nil {
		cur = string(b)
	}
	next, err := ReplaceBlock(cur, want)
	if err != nil {
		return "rewrite atrium's block in " + path, fmt.Errorf("%s: %w", path, err)
	}
	if err := r.M.WriteFile(path, []byte(next), 0o600); err != nil {
		return "write " + path, err
	}
	if admin {
		// sshd ignores this file unless only Administrators and SYSTEM can write it.
		if out, err := r.M.Run(ctx, "icacls", path, "/inheritance:r", "/grant", "Administrators:F", "/grant", "SYSTEM:F"); err != nil {
			return "set the permissions on " + path, fmt.Errorf("icacls said: %s", strings.TrimSpace(out))
		}
	} else if r.GOOS != "windows" {
		if u := r.M.UserHome(r.User); u != r.M.Home() {
			// The room runs as one account and the login is another: the file has to be the login's.
			dir := filepath.Dir(path)
			_, _ = r.M.Run(ctx, "chown", r.User, dir, path)
		}
	}
	return fmt.Sprintf("wrote %d key(s) into atrium's block in %s", len(want), path), nil
}

// ReplaceBlock returns text with atrium's managed block set to want and every other line exactly as it was. No block
// yet puts one at the end. An empty want removes the block. A block that never ends is an error.
func ReplaceBlock(text string, want []wantKey) (string, error) {
	lines := strings.SplitAfter(text, "\n")
	var keep []string
	in := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, beginMark):
			in = true
		case in && strings.HasPrefix(t, endMark):
			in = false
		case !in && l != "":
			keep = append(keep, l)
		}
	}
	if in {
		return "", errors.New("has a `" + beginMark + "` line with no `" + endMark + "` after it. fix it by hand, nothing was changed")
	}
	out := strings.Join(keep, "")
	if len(want) == 0 {
		return out, nil
	}
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	var sb strings.Builder
	sb.WriteString(out)
	sb.WriteString(beginMark + " (managed by `atrium ssh setup`. lines in this block are rewritten, others are never touched)\n")
	for _, w := range want {
		sb.WriteString(w.Body + " atrium:" + w.Room + "\n")
	}
	sb.WriteString(endMark + "\n")
	return sb.String(), nil
}
