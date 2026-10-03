package gitsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// What the hub knows of a branch, for the change record's "Pushed" line and for change requests. See
// docs/rnd/hub-forge-design.md section 6. Every read here is a git command in the hub's own bare repository, run
// with no shell and a bound, on a name and a sha that were checked first.

// The states of Pushed, as the API spells them.
const (
	PushedMatches   = "matches"
	PushedBehind    = "behind"
	PushedAhead     = "ahead"
	PushedDiverged  = "diverged"
	PushedNotPushed = "not-pushed"
)

// readBound is how long one read of the store may take. A commit graph walk over a big repository is a fraction
// of a second, and nothing here may hold a request for longer than this.
const readBound = 15 * time.Second

var shaRe = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// ValidSHA says whether s is a full commit id, the only form a read here takes.
func ValidSHA(s string) bool { return shaRe.MatchString(s) }

// Pushed is where a branch stands on the hub against the head a room has.
type Pushed struct {
	State string `json:"state"`
	// HubSHA is the hub's tip of the branch, empty for not-pushed.
	HubSHA string `json:"hub_sha"`
	// Room and Card are the branch's owner in the push log, both empty for the operator's and for a branch the
	// log has no row for. At is the time of its latest push, RFC 3339, empty with no row.
	Room     string `json:"room"`
	Card     string `json:"card"`
	At       string `json:"at"`
	Released bool   `json:"released"`
}

// exitCode is the exit status of a git command that ran and said no, or -1 for anything else (a failure to run
// it, a timeout, a kill).
func exitCode(err error) int {
	var ge *Error
	if !errors.As(err, &ge) {
		return -1
	}
	var ee *exec.ExitError
	if errors.As(ge.Err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func (s *Store) bareFor(name string) (string, error) {
	dir, err := s.Path(name)
	if err != nil {
		return "", err
	}
	if !isMarked(dir) {
		return "", nil
	}
	return dir, nil
}

// BranchSHA is the hub's tip of a branch, and false when the repository or the branch is not there.
func (s *Store) BranchSHA(ctx context.Context, name, branch string) (string, bool, error) {
	if why := CheckPushBranch(branch); why != "" {
		return "", false, refuse("%s", why)
	}
	dir, err := s.bareFor(name)
	if err != nil || dir == "" {
		return "", false, err
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	return s.tipOf(ctx, dir, branch)
}

func (s *Store) tipOf(ctx context.Context, dir, branch string) (string, bool, error) {
	out, err := s.git(ctx, dir, "for-each-ref", "--format=%(objectname)", headsPrefix+branch)
	if err != nil {
		return "", false, err
	}
	sha := strings.TrimSpace(out)
	if !ValidSHA(sha) {
		return "", false, nil
	}
	return sha, true, nil
}

// hasCommit says whether the repository holds a commit by that id.
func (s *Store) hasCommit(ctx context.Context, dir, sha string) (bool, error) {
	_, err := s.git(ctx, dir, "rev-parse", "--verify", "-q", sha+"^{commit}")
	switch {
	case err == nil:
		return true, nil
	case exitCode(err) == 1:
		return false, nil
	}
	return false, err
}

// isAncestor says whether a is b or a commit b is built on. Both must be commits the repository holds.
func (s *Store) isAncestor(ctx context.Context, dir, a, b string) (bool, error) {
	_, err := s.git(ctx, dir, "merge-base", "--is-ancestor", a, b)
	switch {
	case err == nil:
		return true, nil
	case exitCode(err) == 1:
		return false, nil
	}
	return false, err
}

// Pushed says where the hub's branch stands against head, the sha a room has:
//
//	matches     the hub's tip is head
//	behind      the hub has the branch and not that head (a head the hub has never seen is this), or only a commit
//	            head is built on
//	ahead       head is a commit the hub's tip is built on
//	diverged    the hub holds head and both have moved
//	not-pushed  the hub has no such branch
func (s *Store) Pushed(ctx context.Context, name, branch, head string) (Pushed, error) {
	if !ValidSHA(head) {
		return Pushed{}, refuse("head is a full commit id")
	}
	if why := CheckPushBranch(branch); why != "" {
		return Pushed{}, refuse("%s", why)
	}
	dir, err := s.bareFor(name)
	if err != nil {
		return Pushed{}, err
	}
	if dir == "" {
		return Pushed{State: PushedNotPushed}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	tip, ok, err := s.tipOf(ctx, dir, branch)
	if err != nil {
		return Pushed{}, err
	}
	if !ok {
		return Pushed{State: PushedNotPushed}, nil
	}
	out := Pushed{HubSHA: tip}
	switch {
	case tip == head:
		out.State = PushedMatches
	default:
		known, err := s.hasCommit(ctx, dir, head)
		if err != nil {
			return Pushed{}, err
		}
		out.State = PushedBehind
		if known {
			if up, err := s.isAncestor(ctx, dir, head, tip); err != nil {
				return Pushed{}, err
			} else if up {
				out.State = PushedAhead
			} else if built, err := s.isAncestor(ctx, dir, tip, head); err != nil {
				return Pushed{}, err
			} else if !built {
				out.State = PushedDiverged
			}
		}
	}
	if s.h.PushLog != nil {
		recs, err := s.h.PushLog.Branches(ctx, canonical(name))
		if err != nil {
			return Pushed{}, err
		}
		for _, r := range recs {
			if r.Ref == headsPrefix+branch {
				out.Room, out.Card, out.Released = r.Room, r.Card, r.Released
				if !r.At.IsZero() {
					out.At = r.At.UTC().Format(time.RFC3339)
				}
			}
		}
	}
	return out, nil
}

// canonical is the store's name for a repository, as the push log keys it.
func canonical(name string) string {
	if ref, err := ParseName(name); err == nil {
		return ref.Name()
	}
	return name
}

// Reachable says whether sha is the tip of branch or a commit it is built on, in the hub's store. A repository,
// a branch or a commit the hub does not hold is false, not an error.
func (s *Store) Reachable(ctx context.Context, name, branch, sha string) (bool, error) {
	if !ValidSHA(sha) {
		return false, refuse("a sha is a full commit id")
	}
	if why := CheckPushBranch(branch); why != "" {
		return false, refuse("%s", why)
	}
	dir, err := s.bareFor(name)
	if err != nil || dir == "" {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	tip, ok, err := s.tipOf(ctx, dir, branch)
	if err != nil || !ok {
		return false, err
	}
	known, err := s.hasCommit(ctx, dir, sha)
	if err != nil || !known {
		return false, err
	}
	return s.isAncestor(ctx, dir, sha, tip)
}

// ── a room's branch ─────────────────────────────────────

// roomAdvertBound is what is read of a room's ref advertisement. A room serves claude/* and the branches of its live
// cards, which is a few hundred refs at the very most.
const roomAdvertBound = 4 << 20

// RoomBranch is the tip of a branch in a room's SERVED SET, asked of the room itself the way a fetch through the
// hub would: its advertisement of refs/heads, which the room's own hideRefs have already cut down to what it serves.
// found is false when the room answered and does not serve that branch. A room that is not attached, does not serve
// git, or does not answer is ErrRoomUnreachable, wrapped with a sentence that says which.
func (h *Hub) RoomBranch(ctx context.Context, room, name, branch string) (sha string, found bool, err error) {
	if why := CheckPushBranch(branch); why != "" {
		return "", false, refuse("%s", why)
	}
	ref, err := ParseName(name)
	if err != nil {
		return "", false, err
	}
	if h.Rooms == nil {
		return "", false, fmt.Errorf("%w: this hub has no rooms", ErrRoomUnreachable)
	}
	var info RoomInfo
	attached := false
	for _, a := range h.Rooms.Attached() {
		if strings.EqualFold(a.Name, room) {
			info, attached = a, true
			break
		}
	}
	switch {
	case !attached:
		return "", false, fmt.Errorf("%w: %s is not connected", ErrRoomUnreachable, room)
	case !info.Git:
		return "", false, fmt.Errorf("%w: %s does not serve git (its build predates it)", ErrRoomUnreachable, info.Name)
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+info.Name+"/v1/git/"+ref.Name()+".git/info/refs?service=git-upload-pack", nil)
	if err != nil {
		return "", false, err
	}
	tr := h.Rooms.Transport(info.Name)
	defer closeIdle(tr)
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return "", false, fmt.Errorf("%w: %s did not answer", ErrRoomUnreachable, info.Name)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		if resp.StatusCode == http.StatusNotFound {
			return "", false, nil
		}
		return "", false, fmt.Errorf("%w: %s answered %d", ErrRoomUnreachable, info.Name, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, roomAdvertBound))
	if err != nil {
		return "", false, fmt.Errorf("%w: %s did not finish answering", ErrRoomUnreachable, info.Name)
	}
	return advertisedTip(raw, headsPrefix+branch)
}

// advertisedTip reads a protocol v0 upload-pack advertisement: the service line, a flush, then `<sha> <ref>` lines
// (the first with the capabilities after a NUL) up to a flush.
func advertisedTip(raw []byte, want string) (string, bool, error) {
	rest := raw
	first := true
	flushes := 0
	for len(rest) > 0 {
		data, flush, next, err := readPkt(rest)
		if err != nil {
			return "", false, fmt.Errorf("%w: the room's refs could not be read", ErrRoomUnreachable)
		}
		rest = next
		if flush {
			flushes++
			if flushes >= 2 {
				break
			}
			continue
		}
		if first {
			first = false
			if !bytes.HasPrefix(data, []byte("# service=")) {
				return "", false, fmt.Errorf("%w: the room's refs could not be read", ErrRoomUnreachable)
			}
			continue
		}
		line := string(data)
		if i := strings.IndexByte(line, 0); i >= 0 {
			line = line[:i]
		}
		sha, ref, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && ref == want && ValidSHA(sha) {
			return sha, true, nil
		}
	}
	return "", false, nil
}
