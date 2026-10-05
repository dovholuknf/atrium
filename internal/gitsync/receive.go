package gitsync

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The hub's store over smart HTTP: fetch and push, at `/git/hub/<host>/<owner>/<repo>.git/...`.
// See docs/rnd/hub-forge-design.md 3.2 and 3.4.
//
// ── what runs before git does ───────────────────────────
//
// A push is read here BEFORE it is handed to `git http-backend`. The body is spooled to a temporary file
// under a 500 MB cap, its command list (old, new, ref) is parsed, and every rule that needs no objects is
// applied by the hub: who is pushing, ref names, main, tags, deletes, a stale old sha, case collisions,
// who owns the branch. A push that breaks any of them is answered the way receive-pack answers a refusal
// (`remote: atrium: <sentence>` and `! [remote rejected]`), and git never sees it. That is the safest way
// to refuse before a ref moves, because nothing of git's is involved in the decision.
//
// ONE RULE NEEDS THE OBJECTS, fast-forward, because the new commit is in the pack. That is a hub-owned
// pre-receive hook (core.hooksPath points at a directory the hub writes), which runs after the pack is in
// quarantine and before any ref moves, and refuses the whole push when any updated ref is not a descendant
// of its old value. git's own receive.denyNonFastForwards stays on behind it. The hook is not read from the
// repository, and the repository has no hooks of its own.
//
// ── and the lock ────────────────────────────────────────
//
// The repository's lock (`store:<lowercase name>`, the one Init takes) is held from the rule check through
// the receive-pack run and the log append, so two racing pushes cannot both pass the ownership check.
// Nothing slow is under it: the upload is spooled first, and the question to an owner's room is asked
// before it.

// StorePrefix is where the store is served, on the link's git kind and on the board.
const StorePrefix = "/git/hub/"

// inStoreWideLock, when set, runs inside the store-wide lock after the case check. Tests widen the window with it.
var inStoreWideLock func()

// maxPush is the most a push may carry. The same figure is receive.maxInputSize. A variable for the tests.
var maxPush int64 = 500 << 20

// maxPushCommands bounds the command list read ahead of the pack.
const maxPushCommands = 1 << 20

// cardAsk bounds the question put to an owner's room.
const cardAsk = 8 * time.Second

// Receiver serves the store. Get it from Hub.StoreHandler.
type Receiver struct{ h *Hub }

// StoreHandler is the handler for the hub's store: fetch for any caller with an identity, and push for a
// card of a room or the operator. It serves nothing that is not marked as the store's, so the `git_repos`
// mirrors are not reachable through it. The listener must put a Caller on the context.
func (h *Hub) StoreHandler() http.Handler { return &Receiver{h: h} }

func (rc *Receiver) createOnPush() bool {
	return rc.h.CreateOnPush != nil && rc.h.CreateOnPush()
}

// splitStorePath reads `/git/hub/<host>/<owner>/<repo>.git/<tail>`.
func splitStorePath(p string) (ref Ref, tail string, ok bool) {
	rest, found := strings.CutPrefix(p, StorePrefix)
	if !found {
		return Ref{}, "", false
	}
	i := strings.Index(rest, ".git/")
	if i < 0 {
		return Ref{}, "", false
	}
	name, tail := rest[:i], rest[i+len(".git/"):]
	if strings.Count(name, "/") != 2 {
		return Ref{}, "", false
	}
	ref, err := ParseName(name)
	// The canonical spelling only: `github.com/o/r` is not another route to `github/o/r`.
	if err != nil || ref.Name() != name {
		return Ref{}, "", false
	}
	return ref, tail, true
}

func (rc *Receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	esc := strings.ToLower(r.URL.EscapedPath())
	if strings.Contains(esc, "%2e") || strings.Contains(esc, "%5c") || strings.Contains(esc, "%2f") {
		http.NotFound(w, r)
		return
	}
	ref, tail, ok := splitStorePath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	caller := CallerFrom(r.Context())
	if caller.Kind == CallerNone {
		http.Error(w, "the hub cannot tell who is asking", http.StatusForbidden)
		return
	}
	exe, err := exec.LookPath("git")
	if err != nil {
		http.Error(w, "git is not installed on this machine", http.StatusServiceUnavailable)
		return
	}
	switch tail {
	case "info/refs":
		if r.Method != http.MethodGet {
			http.Error(w, "that has to be a GET", http.StatusForbidden)
			return
		}
		switch r.URL.Query().Get("service") {
		case "git-upload-pack":
			rc.serveFetch(w, r, ref, exe, "/info/refs")
		case "git-receive-pack":
			rc.advertisePush(w, r, ref, exe, caller)
		default:
			http.Error(w, "this serves fetch and push and nothing else", http.StatusForbidden)
		}
	case "git-upload-pack":
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-git-upload-pack-request" {
			http.Error(w, "that has to be a fetch request", http.StatusForbidden)
			return
		}
		rc.serveFetch(w, r, ref, exe, "/git-upload-pack")
	case "git-receive-pack":
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-git-receive-pack-request" {
			http.Error(w, "that has to be a push request", http.StatusForbidden)
			return
		}
		rc.push(w, r, ref, exe, caller)
	default:
		// The dumb protocol, HEAD, objects/..., anything else.
		http.NotFound(w, r)
	}
}

// dir is the store's directory for a repository that is in the store, and false for one that is not. A
// bare repository without the marker (a git_repos mirror) is not, and neither is a name that differs from
// an existing one only in case.
func (rc *Receiver) dir(ref Ref) (string, bool) {
	s := rc.h.Store()
	d := s.dir(ref)
	if !isMarked(d) || s.collision(ref) != "" {
		return "", false
	}
	return d, true
}

// ── config ──────────────────────────────────────────────

// hooksDir is the directory the hub's own hooks are in. git is pointed at it, and never at the repository's.
func (rc *Receiver) hooksDir() string { return filepath.Join(rc.h.Dir, "git-hooks") }

// preReceive refuses a push whose updated ref is not a descendant of its old value, once the pack is in
// quarantine and before any ref moves. The rest of the rules are applied by the hub before git is run.
const preReceive = `#!/bin/sh
# Written by the atrium hub. Run by git after the pack is received and before any ref moves.
# A nonzero exit refuses the whole push and leaves no object behind.
bad=0
while read old new ref; do
	case "$old" in *[!0]*) ;; *) continue ;; esac
	case "$new" in *[!0]*) ;; *) echo "atrium: $ref cannot be deleted by a push" >&2; bad=1; continue ;; esac
	if ! git merge-base --is-ancestor "$old" "$new" 2>/dev/null; then
		echo "atrium: $ref is not a fast-forward of what the hub has. fetch, merge or rebase, and push again" >&2
		bad=1
	fi
done
exit $bad
`

func (rc *Receiver) ensureHooks() (string, error) {
	dir := rc.hooksDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "pre-receive")
	if cur, err := os.ReadFile(path); err != nil || string(cur) != preReceive {
		if err := os.WriteFile(path, []byte(preReceive), 0o755); err != nil {
			return "", err
		}
	}
	// WriteFile keeps the mode of a file that was already there.
	_ = os.Chmod(path, 0o755)
	return dir, nil
}

// baseConfig is what every request to the store runs under, in the CGI environment. Nothing is written to
// a repository. The filter and any-sha settings are the stage 3 hooks, off.
func baseConfig(hooks string) [][2]string {
	return [][2]string{
		{"http.getanyfile", "false"},
		{"http.uploadarch", "false"},
		{"core.hooksPath", filepath.ToSlash(hooks)},
		{"core.protectNTFS", "true"},
		{"core.protectHFS", "true"},
		{"uploadpack.allowFilter", "false"},
		{"uploadpack.allowAnySHA1InWant", "false"},
		{"uploadpack.allowTipSHA1InWant", "false"},
		{"uploadpack.allowReachableSHA1InWant", "false"},
	}
}

func fetchConfig(hooks string) [][2]string {
	return append(baseConfig(hooks), [2]string{"http.receivepack", "false"})
}

func pushConfig(hooks string) [][2]string {
	return append(baseConfig(hooks),
		[2]string{"http.receivepack", "true"},
		[2]string{"receive.denyNonFastForwards", "true"},
		[2]string{"receive.denyDeletes", "true"},
		[2]string{"receive.denyDeleteCurrent", "true"},
		[2]string{"receive.fsckObjects", "true"},
		[2]string{"receive.maxInputSize", strconv.FormatInt(maxPush, 10)},
		[2]string{"receive.advertisePushOptions", "false"},
		[2]string{"receive.shallowUpdate", "false"},
	)
}

// cgiEnv keeps the operator's git configuration out of the CGI.
func cgiEnv() []string {
	return []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_ASKPASS="}
}

// forGit is the request git sees: protocol v0, no atrium header, no credential, and the path of the one
// directory.
func forGit(r *http.Request, dir, svc string) *http.Request {
	out := r.Clone(r.Context())
	for _, h := range []string{"Git-Protocol", HeaderCard, HeaderChain, "Authorization", "Cookie"} {
		out.Header.Del(h)
	}
	out.URL.Path = "/" + filepath.Base(dir) + svc
	out.URL.RawPath = ""
	return out
}

// ── fetch ───────────────────────────────────────────────

func (rc *Receiver) serveFetch(w http.ResponseWriter, r *http.Request, ref Ref, exe, svc string) {
	dir, ok := rc.dir(ref)
	if !ok {
		http.NotFound(w, r)
		return
	}
	hooks, err := rc.ensureHooks()
	if err != nil {
		http.Error(w, "the hub could not prepare to serve that", http.StatusInternalServerError)
		return
	}
	out := forGit(r, dir, svc)
	if svc == "/git-upload-pack" {
		body, err := readBody(r, maxRequest)
		if err != nil {
			rc.badBody(w, err)
			return
		}
		why, err := fetchRefusal(body)
		if err != nil {
			http.Error(w, "that is not a git fetch request", http.StatusBadRequest)
			return
		}
		if why != "" {
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(pktLine("ERR atrium: " + why + "\n"))
			return
		}
		out.Body = io.NopCloser(bytes.NewReader(body))
		out.ContentLength = int64(len(body))
		out.TransferEncoding = nil
		out.Header.Del("Transfer-Encoding")
		out.Header.Del("Content-Encoding")
		out.Header.Set("Content-Length", strconv.Itoa(len(body)))
	}
	serveCGI(gitCGI(exe, dir, fetchConfig(hooks), cgiEnv()), w, out)
}

var errTooBig = errors.New("too big")

// readBody reads a request body, undoing gzip, up to limit bytes.
func readBody(r *http.Request, limit int64) ([]byte, error) {
	rd, err := bodyReader(r)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(rd, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errTooBig
	}
	return raw, nil
}

func bodyReader(r *http.Request) (io.Reader, error) {
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Content-Encoding")), "gzip") {
		return gzip.NewReader(r.Body)
	}
	return r.Body, nil
}

func (rc *Receiver) badBody(w http.ResponseWriter, err error) {
	if errors.Is(err, errTooBig) {
		w.Header().Set("Connection", "close")
		http.Error(w, "that request is too large", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, "that request could not be read", http.StatusBadRequest)
}

// ── push: the advertisement ─────────────────────────────

// advertisePush answers GET info/refs?service=git-receive-pack. A refusal is an `ERR` line in the
// advertisement, which git prints as `fatal: remote error: atrium: <sentence>`. A repository the hub lacks
// is advertised as empty, from a throwaway one, and is made only when the push itself arrives.
func (rc *Receiver) advertisePush(w http.ResponseWriter, r *http.Request, ref Ref, exe string, c Caller) {
	adv := func(why string) {
		w.Header().Set("Content-Type", "application/x-git-receive-pack-advertisement")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(refusedAdvert("git-receive-pack", why))
	}
	if rc.h.PushLog == nil {
		http.Error(w, "this hub keeps no push log, so it takes no pushes", http.StatusServiceUnavailable)
		return
	}
	if _, why := pusherFrom(r, c); why != "" {
		adv(why)
		return
	}
	hooks, err := rc.ensureHooks()
	if err != nil {
		http.Error(w, "the hub could not prepare to serve that", http.StatusInternalServerError)
		return
	}
	dir, ok := rc.dir(ref)
	if ok && markerKind(dir) == KindAdopted {
		adv(adoptedSentence(ref.Name()))
		return
	}
	if !ok {
		if why := rc.cannotCreate(ref, c.Kind == CallerOperator); why != "" {
			adv(why)
			return
		}
		tmp, err := os.MkdirTemp("", "atrium-advert-*")
		if err != nil {
			http.Error(w, "the hub could not prepare to serve that", http.StatusInternalServerError)
			return
		}
		defer os.RemoveAll(tmp)
		dir = filepath.Join(tmp, "p.git")
		if _, err := rc.h.Store().git(r.Context(), "", "init", "-q", "--bare", "--template=", dir); err != nil {
			http.Error(w, "the hub could not prepare to serve that", http.StatusInternalServerError)
			return
		}
	}
	serveCGI(gitCGI(exe, dir, pushConfig(hooks), cgiEnv()), w, forGit(r, dir, "/info/refs"))
}

// cannotCreate says why a push could not make this repository, or "". A repository that is in the store
// is not asked about.
func (rc *Receiver) cannotCreate(ref Ref, operator bool) string {
	s := rc.h.Store()
	switch {
	case operator:
		return ref.Name() + " is not in the hub's store. the operator makes it with `atrium rooms git init <url>`, and pushes after"
	case !rc.createOnPush():
		return ref.Name() + " is not in the hub's store, and this hub does not make a repository on a push. " +
			"ask the operator to run `atrium rooms git init <url>` for it"
	}
	if hit := s.collision(ref); hit != "" {
		return ref.Name() + " cannot be made, because " + hit + " is in the store and a disk that ignores case would make them one"
	}
	if _, err := os.Stat(s.dir(ref)); err == nil {
		return ref.Name() + " cannot be made, because something is already at that name that the store did not make. " +
			"the operator takes it in with `atrium rooms git init <url>`"
	}
	return ""
}

// ── push ────────────────────────────────────────────────

// spool copies the request body to a temporary file, undoing gzip, up to the cap.
func spool(r *http.Request) (*os.File, int64, error) {
	if r.ContentLength > maxPush {
		return nil, 0, errTooBig
	}
	rd, err := bodyReader(r)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.CreateTemp("", "atrium-push-*")
	if err != nil {
		return nil, 0, err
	}
	n, err := io.Copy(f, io.LimitReader(rd, maxPush+1))
	if err == nil && n > maxPush {
		err = errTooBig
	}
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, 0, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, 0, err
	}
	return f, n, nil
}

type bufWriter struct {
	h    http.Header
	code int
	buf  bytes.Buffer
}

func (b *bufWriter) Header() http.Header { return b.h }
func (b *bufWriter) WriteHeader(c int) {
	if b.code == 0 {
		b.code = c
	}
}
func (b *bufWriter) Write(p []byte) (int, error) {
	b.WriteHeader(http.StatusOK)
	return b.buf.Write(p)
}

// reset forgets what was written, for an answer that replaces what git said.
func (b *bufWriter) reset() {
	b.h, b.code = http.Header{}, 0
	b.buf.Reset()
}

func (b *bufWriter) copyTo(w http.ResponseWriter) {
	for k, v := range b.h {
		w.Header()[k] = v
	}
	if b.code != 0 {
		w.WriteHeader(b.code)
	}
	_, _ = w.Write(b.buf.Bytes())
}

func (rc *Receiver) push(w http.ResponseWriter, r *http.Request, ref Ref, exe string, c Caller) {
	ctx := r.Context()
	if rc.h.PushLog == nil {
		http.Error(w, "this hub keeps no push log, so it takes no pushes", http.StatusServiceUnavailable)
		return
	}
	f, size, err := spool(r)
	if err != nil {
		rc.badBody(w, err)
		return
	}
	defer func() {
		f.Close()
		os.Remove(f.Name())
	}()

	head := make([]byte, min(size, maxPushCommands))
	if _, err := f.ReadAt(head, 0); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "that push could not be read", http.StatusBadRequest)
		return
	}
	req, err := parsePush(head)
	if err != nil {
		http.Error(w, "that is not a git push", http.StatusBadRequest)
		return
	}

	// FROM HERE EVERYTHING IS ANSWERED INTO resp, AND WRITTEN TO THE CLIENT AFTER THE LOCK IS LET GO: a client that
	// stops reading must not hold the repository. (Deferred first, so it runs after the unlock.)
	resp := &bufWriter{h: http.Header{}}
	defer resp.copyTo(w)
	refuseAll := func(why string) {
		bad := map[string]string{}
		for _, u := range req.Updates {
			bad[u.Ref] = why
		}
		rc.h.audit(c.Room, "git-hub-push-refused", fmt.Sprintf("%s refs=%d", ref.Name(), len(req.Updates)))
		resp.reset()
		resp.Header().Set("Content-Type", "application/x-git-receive-pack-result")
		resp.Header().Set("Cache-Control", "no-cache")
		_, _ = resp.Write(refusedPush(req, bad, why))
	}
	fail := func(code int, msg string) {
		resp.reset()
		http.Error(resp, msg, code)
	}
	if req.Why != "" {
		refuseAll(req.Why)
		return
	}
	who, why := pusherFrom(r, c)
	if why != "" {
		refuseAll(why)
		return
	}
	hooks, err := rc.ensureHooks()
	if err != nil {
		fail(http.StatusInternalServerError, "the hub could not prepare to take that")
		return
	}

	// DONE BEFORE THE LOCK, none of it depends on what the lock holds: the verdict on each ref's name (a process
	// per ref), and what the owners' rooms say about the cards that own the branches this push names. The owner
	// is re-read under the lock, and an answer is used only for the owner it was asked about.
	names := rc.checkNames(ctx, req)
	asked := rc.askOwners(ctx, ref.Name(), who, req)

	name := ref.Name()
	l := rc.h.lock("store:" + strings.ToLower(name))
	l.Lock()
	defer l.Unlock()

	dir, exists := rc.dir(ref)
	cur := map[string]string{}
	if exists {
		// A git_repos mirror the operator took in is under the mirror pass's lock as well, as Init takes it.
		if ml := rc.h.Store().mirrorLock(dir); ml != nil {
			ml.Lock()
			defer ml.Unlock()
		}
		// NOBODY PUSHES INTO AN ADOPTED MIRROR, the operator included: the mirror pass force-fetches over it.
		if markerKind(dir) == KindAdopted {
			refuseAll(adoptedSentence(name))
			return
		}
	} else if why := rc.cannotCreate(ref, who.Operator); why != "" {
		refuseAll(why)
		return
	}
	// What a crash, or a Settle that failed, left pending in this repository is settled now, before this push
	// reads who owns what.
	leftDir := ""
	if exists {
		leftDir = dir
	}
	if err := rc.settleLeftovers(ctx, name, leftDir); err != nil {
		fail(http.StatusInternalServerError, "the hub could not read its record of this repository")
		return
	}
	if exists {
		if cur, err = rc.readRefs(ctx, dir); err != nil {
			fail(http.StatusInternalServerError, "the hub could not read that repository")
			return
		}
	}

	dec := rc.decide(ctx, name, who, req, cur, names, asked)
	if len(dec.bad) > 0 {
		rc.h.audit(who.Room, "git-hub-push-refused", fmt.Sprintf("%s refs=%d card=%s", name, len(req.Updates), who.Card))
		resp.reset()
		resp.Header().Set("Content-Type", "application/x-git-receive-pack-result")
		resp.Header().Set("Cache-Control", "no-cache")
		_, _ = resp.Write(refusedPush(req, dec.bad, dec.sentence()))
		return
	}

	created := false
	if !exists {
		dir = rc.h.Store().dir(ref)
		// THE STORE-WIDE LOCK around the case check and the directories that make it true, after the
		// repository's own, exactly as Init does. Released before create, which takes it to tidy up a failure.
		gl := rc.h.lock(storeWideLock)
		gl.Lock()
		hit := rc.h.Store().collision(ref)
		_, statErr := os.Stat(dir)
		if inStoreWideLock != nil {
			inStoreWideLock()
		}
		var mkErr error
		if hit == "" && statErr != nil {
			mkErr = os.MkdirAll(filepath.Dir(dir), 0o755)
		}
		gl.Unlock()
		switch {
		case hit != "":
			refuseAll(ref.Name() + " cannot be made, because " + hit + " is in the store and a disk that ignores case would make them one")
			return
		case statErr == nil:
			refuseAll(ref.Name() + " cannot be made, because something is already at that name that the store did not make")
			return
		case mkErr != nil:
			fail(http.StatusInternalServerError, "the hub could not make that repository")
			return
		}
		if err := rc.h.Store().create(ctx, dir); err != nil {
			fail(http.StatusInternalServerError, "the hub could not make that repository")
			return
		}
		created = true
		rc.h.audit(who.Room, "git-store-created-on-push", fmt.Sprintf("%s card=%s", name, who.Card))
	}
	// A repository this push made, and that took nothing, is taken away again.
	moved := false
	defer func() {
		if created && !moved {
			rc.removeCreated(dir)
		}
	}()

	// THE PUSH IS WRITTEN PENDING BEFORE GIT RUNS. A pending row counts as the owner of its branch, so a hub that
	// dies between git moving a ref and the row being settled leaves the branch owned, and startup settles it. A
	// branch the pusher takes over carries what released it, and the release is written when the push lands, with
	// the same transaction, so a push that git refuses releases nothing.
	now := time.Now().UTC()
	var rows []PushRow
	for _, u := range req.Updates {
		if cur[u.Ref] == u.New {
			continue
		}
		row := PushRow{Repo: name, Ref: u.Ref, Old: u.Old, New: u.New, Room: who.Room, Card: who.Card, At: now}
		for _, rel := range dec.releases {
			if rel.ref == u.Ref {
				row.ReleasedBy = rel.by
			}
		}
		rows = append(rows, row)
	}
	batch := ""
	if len(rows) > 0 {
		if batch, err = rc.h.PushLog.Begin(ctx, rows...); err != nil {
			rc.h.audit(who.Room, "git-hub-push-failed", fmt.Sprintf("%s the push log could not be written", name))
			refuseAll("the hub could not record that push, so it was not taken. try again")
			return
		}
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		fail(http.StatusInternalServerError, "that push could not be read")
		return
	}
	out := forGit(r, dir, "/git-receive-pack")
	out.Body = f
	out.ContentLength = size
	out.TransferEncoding = nil
	out.Header.Del("Transfer-Encoding")
	out.Header.Del("Content-Encoding")
	out.Header.Set("Content-Length", strconv.FormatInt(size, 10))

	serveCGI(gitCGI(exe, dir, pushConfig(hooks), cgiEnv()), resp, out)

	// THE REFS ARE THE RESULT. Whatever git said, a push is settled for exactly the refs that now hold the
	// sha it asked for.
	after, err := rc.readRefs(ctx, dir)
	if err != nil {
		// Left pending, counted as the owner, and settled by the next push to this repository or at startup.
		moved = true
		fail(http.StatusInternalServerError, "the hub could not read that repository")
		return
	}
	var landed []string
	for _, row := range rows {
		if after[row.Ref] == row.New {
			landed = append(landed, row.Ref)
		}
	}
	moved = len(landed) > 0
	if batch != "" {
		if err := rc.h.PushLog.Settle(ctx, batch, landed...); err != nil {
			// The rows stay pending. They count as owners, so nothing is lost, and the next push to this
			// repository or a restart settles them from the refs.
			rc.h.audit(who.Room, "git-hub-push-unsettled", fmt.Sprintf("%s refs=%d the push log could not settle it", name, len(landed)))
			return
		}
	}
	for _, row := range rows {
		if after[row.Ref] != row.New {
			continue
		}
		if row.ReleasedBy != "" {
			rc.h.audit(who.Room, "git-hub-branch-released", fmt.Sprintf("%s %s by=%s", name, row.Ref, row.ReleasedBy))
		}
		rc.h.audit(who.Room, "git-hub-push", fmt.Sprintf("%s %s %s..%s card=%s", name, row.Ref, short(row.Old), short(row.New), who.Card))
	}
	if len(landed) == 0 && len(rows) > 0 {
		rc.h.audit(who.Room, "git-hub-push-failed", fmt.Sprintf("%s refs=%d card=%s", name, len(req.Updates), who.Card))
	}
}

// settleLeftovers settles what a crash, or a Settle that failed, left pending in a repository. A pending row whose
// ref now holds the sha it was to take is done, and anything else (the ref is where it was, or is not there, or the
// repository is gone) is dropped. The repository's lock is held. dir is "" for a repository that is not on disk.
func (rc *Receiver) settleLeftovers(ctx context.Context, repo, dir string) error {
	pend, err := rc.h.PushLog.Pending(ctx, repo)
	if err != nil || len(pend) == 0 {
		return err
	}
	var cur map[string]string
	if dir != "" {
		if cur, err = rc.readRefs(ctx, dir); err != nil {
			return err
		}
	}
	landed := map[string][]string{}
	var order []string
	for _, r := range pend {
		if _, seen := landed[r.Batch]; !seen {
			order = append(order, r.Batch)
			landed[r.Batch] = nil
		}
		if r.New != "" && cur[r.Ref] == r.New {
			landed[r.Batch] = append(landed[r.Batch], r.Ref)
		}
	}
	for _, b := range order {
		if err := rc.h.PushLog.Settle(ctx, b, landed[b]...); err != nil {
			return err
		}
		rc.h.audit("", "git-hub-push-reconciled", fmt.Sprintf("%s batch=%s taken=%d", repo, b, len(landed[b])))
	}
	return nil
}

// Reconcile settles every push the log still has pending, at startup: the hub may have died between git moving a
// ref and the push being settled. Each repository is settled under its own lock.
func (h *Hub) Reconcile(ctx context.Context) error {
	if h.PushLog == nil {
		return nil
	}
	pend, err := h.PushLog.Pending(ctx, "")
	if err != nil || len(pend) == 0 {
		return err
	}
	rc := &Receiver{h: h}
	done := map[string]bool{}
	for _, r := range pend {
		if done[r.Repo] {
			continue
		}
		done[r.Repo] = true
		ref, err := ParseName(r.Repo)
		if err != nil {
			continue
		}
		func() {
			l := h.lock("store:" + strings.ToLower(ref.Name()))
			l.Lock()
			defer l.Unlock()
			dir, ok := rc.dir(ref)
			if !ok {
				dir = ""
			}
			if e := rc.settleLeftovers(ctx, r.Repo, dir); e != nil && err == nil {
				err = e
			}
		}()
	}
	return err
}

// readRefs is every branch and tag in a repository, by full name.
func (rc *Receiver) readRefs(ctx context.Context, dir string) (map[string]string, error) {
	out, err := rc.h.Store().git(ctx, dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads", "refs/tags")
	if err != nil {
		return nil, err
	}
	refs := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) == 2 {
			refs[f[0]] = f[1]
		}
	}
	return refs, nil
}
