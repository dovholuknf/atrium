package gitsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// A forge's branch in the hub's store, fetched when a card asks for it. See docs/fabric/hub-forge-design.md 3.5.
//
// A BRANCH NEITHER THE STORE NOR A ROOM HAS is fetched from the repository's forge (the URL the main seed reads) into
// `refs/forge/<branch>`, a namespace of its own. Never refs/heads: a room's pushed work and main are never written over,
// and a room may still push a branch of the same name, which then wins. The store's fetch route advertises the forge's
// copy under `refs/heads/<branch>` as well, when no pushed branch of that name is there (forgeAdvert), so
// `git fetch hub <branch>` just works, and `git fetch hub forge/<branch>` always names the forge's copy.
//
// WHAT TRIGGERS IT. The lookup (atrium_git_url with a branch) fetches a branch the first time. The fetch route cannot:
// a protocol v0 fetch names no branch before the hub has advertised its refs, and v2 is not spoken. So the route only
// refreshes the forge branches the store already holds, before it advertises them, so a colleague's new commits arrive.
//
// BOUNDED: one fetch per branch at a time (a second ask waits for the first and takes its answer), the seed's timeout
// on every fetch, forgeRefreshWait on the route's refresh, at most ForgeBranchMax branches per repository, and a refresh
// at most once per forgeFreshFor. READ-ONLY: every transfer is a fetch from the forge, and nothing is pushed to one.
//
// CREDENTIALS are the PR head fetch's (storepr.go): the forge CLI's helper for this one command (Hub.ForgeHelper), and
// none for a public repository. A private one with no helper is answered "the hub has no credential for <repo>".

// ForgeRefPrefix is where the store keeps the forge's branches.
const ForgeRefPrefix = "refs/forge/"

const (
	// ForgeBranchMax is the most forge branches the store holds for one repository.
	ForgeBranchMax = 100
	// forgeFreshFor is how long the route trusts what it last read of the forge.
	forgeFreshFor = 10 * time.Second
	// forgeRefreshWait bounds the route's refresh, so a slow forge holds up a fetch by this much at most. What the
	// store already has is served when it runs out.
	forgeRefreshWait = 20 * time.Second
)

// ErrNoForgeBranch is a forge that answered, and has no branch of that name.
var ErrNoForgeBranch = errors.New("the forge has no branch of that name")

// forgeState is the store's fetches in flight and the time of each repository's last refresh.
type forgeState struct {
	mu      sync.Mutex
	flights map[string]*forgeFlight
	at      map[string]time.Time
}

type forgeFlight struct {
	done chan struct{}
	sha  string
	err  error
}

// once runs f for key unless a run for it is in flight, in which case it waits for that run and answers its result.
func (fs *forgeState) once(ctx context.Context, key string, f func() (string, error)) (string, error) {
	fs.mu.Lock()
	if fl, ok := fs.flights[key]; ok {
		fs.mu.Unlock()
		select {
		case <-fl.done:
			return fl.sha, fl.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if fs.flights == nil {
		fs.flights = map[string]*forgeFlight{}
	}
	fl := &forgeFlight{done: make(chan struct{})}
	fs.flights[key] = fl
	fs.mu.Unlock()
	fl.sha, fl.err = f()
	fs.mu.Lock()
	delete(fs.flights, key)
	fs.mu.Unlock()
	close(fl.done)
	return fl.sha, fl.err
}

// due says whether a repository's forge branches were last read more than forgeFreshFor ago, and if so marks them read.
func (fs *forgeState) due(key string) bool {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if at, ok := fs.at[key]; ok && time.Since(at) < forgeFreshFor {
		return false
	}
	if fs.at == nil {
		fs.at = map[string]time.Time{}
	}
	fs.at[key] = time.Now()
	return true
}

// forgeBranchOK says whether a branch name may be asked of a forge: one git takes under refs/heads, that is no option.
func (s *Store) forgeBranchOK(ctx context.Context, branch string) bool {
	if branch == "" || len(branch) > 200 || strings.HasPrefix(branch, "-") || strings.ContainsAny(branch, " \t\r\n\x00:") {
		return false
	}
	_, err := s.git(ctx, "", "check-ref-format", headsPrefix+branch)
	return err == nil
}

// helper is the credential helper for a repository's forge, or "" for none.
func (s *Store) helper(ref Ref) string {
	if s.h.ForgeHelper == nil {
		return ""
	}
	return s.h.ForgeHelper(forgeHost(ref))
}

func (s *Store) forgeBound() time.Duration {
	if s.SeedTimeout > 0 {
		return s.SeedTimeout
	}
	return 2 * time.Minute
}

// credArgs set helper for one command, after every configured helper is reset, as prFetchArgs does.
func credArgs(helper string) []string {
	if helper == "" {
		return nil
	}
	return []string{"-c", "credential.helper=", "-c", "credential.helper=" + helper}
}

// forgeHeads asks the forge for the tips of branches, by one ls-remote. A branch it does not have is not in the answer.
func (s *Store) forgeHeads(ctx context.Context, ref Ref, helper string, branches []string) (map[string]string, error) {
	args := append(credArgs(helper), "ls-remote", "--heads", "--", s.forgeURL(ref))
	for _, b := range branches {
		args = append(args, headsPrefix+b)
	}
	out, err := s.git(ctx, "", args...)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, b := range branches {
		want[b] = true
	}
	got := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		sha, name, ok := strings.Cut(strings.TrimSpace(l), "\t")
		b, isHead := strings.CutPrefix(name, headsPrefix)
		// ls-remote's patterns match on a tail, so only the exact names asked for are taken.
		if ok && isHead && isHex40(sha) && want[b] {
			got[b] = sha
		}
	}
	return got, nil
}

// HasForgeBranch asks the forge whether a repository the store may not hold yet has a branch. It is how the lookup
// decides to make a repository for a first ask, so a typo makes nothing.
func (s *Store) HasForgeBranch(ctx context.Context, ref Ref, branch string) (bool, error) {
	if !s.forgeBranchOK(ctx, branch) {
		return false, refuse("%s is not a branch name git will take", shown(branch))
	}
	ctx, cancel := context.WithTimeout(ctx, s.forgeBound())
	defer cancel()
	helper := s.helper(ref)
	got, err := s.forgeHeads(ctx, ref, helper, []string{branch})
	if err != nil {
		return false, s.forgeFailed(ref, helper, err)
	}
	_, ok := got[branch]
	return ok, nil
}

// FetchForgeBranch fetches one branch of a repository the store holds from its forge into refs/forge/<branch>, and
// answers its sha. A branch the store already has at the forge's tip is not fetched again. ErrNoForgeBranch is a forge
// that has no such branch, and then a copy the store held is removed. A *FetchError with Auth is a credential the hub
// does not have, or one the forge did not take.
func (s *Store) FetchForgeBranch(ctx context.Context, name, branch string) (string, error) {
	ref, err := ParseName(name)
	if err != nil {
		return "", err
	}
	if !s.forgeBranchOK(ctx, branch) {
		return "", refuse("%s is not a branch name git will take", shown(branch))
	}
	dir, ok := (&Receiver{h: s.h}).dir(ref)
	if !ok {
		return "", refuse("the store does not hold %s", ref.Name())
	}
	if markerKind(dir) == KindAdopted {
		return "", refuse("%s is a mirror the hub serves as it is, so nothing is fetched into it", ref.Name())
	}
	// ONE CARD'S CANCEL DOES NOT FAIL ANOTHER'S WAIT: the fetch runs on its own deadline.
	run := context.WithoutCancel(ctx)
	return s.forge.once(ctx, strings.ToLower(ref.Name())+"\x00"+branch, func() (string, error) {
		run, cancel := context.WithTimeout(run, s.forgeBound())
		defer cancel()
		return s.fetchForge(run, ref, dir, branch)
	})
}

func (s *Store) fetchForge(ctx context.Context, ref Ref, dir, branch string) (string, error) {
	helper := s.helper(ref)
	held, err := listRefs(ctx, s.h.runner(), dir, ForgeRefPrefix)
	if err != nil {
		return "", err
	}
	dst := ForgeRefPrefix + branch
	// THE FORGE IS ASKED FIRST, with no lock held, so a branch already at its tip costs a push nothing.
	got, err := s.forgeHeads(ctx, ref, helper, []string{branch})
	if err != nil {
		return "", s.forgeFailed(ref, helper, err)
	}
	sha, ok := got[branch]
	if !ok {
		if _, had := held[dst]; had {
			s.forgeUpdate(ctx, ref, dir, helper, nil, []string{branch})
		}
		return "", ErrNoForgeBranch
	}
	if held[dst] == sha {
		return sha, nil
	}
	if _, had := held[dst]; !had {
		for r := range held {
			// NTFS keeps a loose ref as a file, so `Fix` and `fix` would be one.
			if strings.EqualFold(r, dst) {
				return "", refuse("the hub holds %s from the forge already, and a disk that ignores case would make "+
					"them one", strings.TrimPrefix(r, "refs/"))
			}
		}
		if len(held) >= ForgeBranchMax {
			return "", refuse("the hub holds %d of %s's forge branches already, which is as many as it keeps",
				len(held), ref.Name())
		}
	}
	if err := s.forgeUpdate(ctx, ref, dir, helper, []string{branch}, nil); err != nil {
		return "", err
	}
	out, err := s.git(ctx, dir, "rev-parse", "--verify", "-q", dst+"^{commit}")
	if err != nil {
		return "", &FetchError{Msg: "the fetch finished and " + dst + " is not there"}
	}
	return strings.TrimSpace(out), nil
}

// forgeUpdate fetches branches from the forge into refs/forge/, and removes the copies of gone ones, under the
// repository's lock, so no push is mid-way through moving a ref. The forge may force-push its own branches, and the
// copy follows, because refs/forge/ is nobody's work but the forge's.
func (s *Store) forgeUpdate(ctx context.Context, ref Ref, dir, helper string, fetch, gone []string) error {
	l := s.h.lock("store:" + strings.ToLower(ref.Name()))
	l.Lock()
	defer l.Unlock()
	if len(fetch) > 0 {
		specs := make([]string, 0, len(fetch))
		for _, b := range fetch {
			specs = append(specs, "+"+headsPrefix+b+":"+ForgeRefPrefix+b)
		}
		if _, err := s.git(ctx, dir, prFetchArgs(s.forgeURL(ref), helper, specs)...); err != nil {
			return s.forgeFailed(ref, helper, err)
		}
		s.h.audit("", "git-store-forge", ref.Name()+" "+strings.Join(fetch, " "))
	}
	for _, b := range gone {
		if _, err := s.git(ctx, dir, "update-ref", "-d", ForgeRefPrefix+b); err == nil {
			s.h.audit("", "git-store-forge-gone", ref.Name()+" "+b)
		}
	}
	return nil
}

// refreshForge brings the forge branches the store holds for a repository up to the forge's tips, before the route
// advertises them. A branch a room pushed under the same name is served instead, so its copy is not refreshed. It is
// best effort: a forge that cannot be asked leaves what the store has, and the fetch goes on.
func (s *Store) refreshForge(ctx context.Context, ref Ref, dir string) {
	if markerKind(dir) == KindAdopted {
		return
	}
	held, err := listRefs(ctx, s.h.runner(), dir, ForgeRefPrefix)
	if err != nil || len(held) == 0 {
		return
	}
	key := strings.ToLower(ref.Name()) + "\x00*"
	if !s.forge.due(key) {
		return
	}
	heads, _ := listRefs(ctx, s.h.runner(), dir, headsPrefix)
	var branches []string
	for r := range held {
		b := strings.TrimPrefix(r, ForgeRefPrefix)
		if _, pushed := heads[headsPrefix+b]; !pushed {
			branches = append(branches, b)
		}
	}
	if len(branches) == 0 {
		return
	}
	sort.Strings(branches)
	run, cancel := context.WithTimeout(context.WithoutCancel(ctx), forgeRefreshWait)
	defer cancel()
	_, _ = s.forge.once(ctx, key, func() (string, error) {
		helper := s.helper(ref)
		got, err := s.forgeHeads(run, ref, helper, branches)
		if err != nil {
			return "", err
		}
		var fetch, gone []string
		for _, b := range branches {
			switch sha, ok := got[b]; {
			case !ok:
				gone = append(gone, b)
			case sha != held[ForgeRefPrefix+b]:
				fetch = append(fetch, b)
			}
		}
		if len(fetch) == 0 && len(gone) == 0 {
			return "", nil
		}
		return "", s.forgeUpdate(run, ref, dir, helper, fetch, gone)
	})
}

// forgeFailed says what went wrong with the forge in a sentence that names the hub. A credential the forge wanted is
// Auth, and with no helper to give one it is said plainly: the hub has no credential for the repository.
func (s *Store) forgeFailed(ref Ref, helper string, err error) error {
	if errors.Is(err, ErrStopped) || errors.Is(err, context.Canceled) {
		return err
	}
	var fe *FetchError
	if errors.As(err, &fe) {
		return err
	}
	text := strings.ToLower(err.Error())
	var ge *Error
	if errors.As(err, &ge) {
		text = strings.ToLower(ge.Stderr + " " + ge.Error())
	}
	for _, w := range []string{"could not read username", "could not read password", "authentication failed",
		"terminal prompts disabled", "returned error: 401", "returned error: 403", "access denied",
		"repository not found", "invalid credentials"} {
		if strings.Contains(text, w) {
			if helper == "" {
				return &FetchError{Auth: true, Msg: "the hub has no credential for " + ref.Name() +
					" (it is private, or the forge has no such repository). the operator can log the hub's forge CLI in"}
			}
			return &FetchError{Auth: true, Msg: "the hub's forge login cannot read " + ref.Name() +
				" (it is private and the login has no access, or the forge has no such repository)"}
		}
	}
	line := err.Error()
	if ge != nil && ge.First() != "" {
		line = ge.First()
	}
	return &FetchError{Msg: "the hub could not fetch from the forge for " + ref.Name() + ": " + s.scrub(line)}
}

// forgeAdvert adds, to an upload-pack ref advertisement of the store, `refs/heads/<b>` for each `refs/forge/<b>` that no
// pushed branch has the name of (without regard to case, for NTFS). git's own upload-pack serves the want, because the
// sha is the tip of refs/forge/<b>, which it advertised itself. An advertisement that does not read is left as it is.
func forgeAdvert(body []byte) []byte {
	if !bytes.Contains(body, []byte(" "+ForgeRefPrefix)) {
		return body
	}
	rest := body
	data, flush, rest, err := readPkt(rest)
	if err != nil || flush || !strings.HasPrefix(string(data), "# service=git-upload-pack") {
		return body
	}
	if _, flush, rest, err = readPkt(rest); err != nil || !flush {
		return body
	}
	heads := map[string]bool{}
	type forged struct{ sha, branch string }
	var forges []forged
	for {
		if len(rest) == 0 {
			return body
		}
		data, flush, next, err := readPkt(rest)
		if err != nil {
			return body
		}
		if flush {
			break
		}
		rest = next
		line := strings.TrimSuffix(string(data), "\n")
		if i := strings.IndexByte(line, 0); i >= 0 {
			line = line[:i]
		}
		sha, name, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		if b, ok := strings.CutPrefix(name, headsPrefix); ok {
			heads[strings.ToLower(b)] = true
		}
		if b, ok := strings.CutPrefix(name, ForgeRefPrefix); ok && isHex40(sha) && b != "" {
			forges = append(forges, forged{sha, b})
		}
	}
	at := len(body) - len(rest)
	var add bytes.Buffer
	for _, f := range forges {
		if !heads[strings.ToLower(f.branch)] {
			heads[strings.ToLower(f.branch)] = true
			add.Write(pktLine(fmt.Sprintf("%s %s%s\n", f.sha, headsPrefix, f.branch)))
		}
	}
	if add.Len() == 0 {
		return body
	}
	out := make([]byte, 0, len(body)+add.Len())
	out = append(out, body[:at]...)
	out = append(out, add.Bytes()...)
	return append(out, body[at:]...)
}
