package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// THE HUB RUNS THE FORGE FOR ITS ROOMS. Design: docs/rnd/scm-forge-design.md, Built.
//
//	POST /_forge/pr     (a room, on the git kind)  forge.HubAsk   a pull request: view, diff, head, and its head fetched
//	POST /_forge/issue  (a room, on the git kind)  forge.HubAsk   an issue
//	POST /_forge/repo   (a room, on the git kind)  forge.HubAsk   a repository the store is to hold
//	GET  /_hub/forge                               the hub's forge entries per host
//	PUT  /_hub/forge                               {entries: [{host, forge, cmd}]}
//	POST /_hub/forge/check                         {forges: [{tool, host, scopes}]}   the hub's CLI logins
//
// clint's rule: rooms use only the hub, and only the hub talks to GitHub or Bitbucket. A room never runs gh or bb and
// never fetches from a forge host. The hub only reads: it views, diffs and fetches, and never pushes to a forge. The
// gh and bb logins live on the hub alone, so a missing one is the hub's alert, a growler on the board, and the
// sentence names the hub.
//
// THE FORGE IS PICKED HERE BY HOST, from the hub's own entries, so a provider's name need not match on every room.

// ForgePrefix is where a room's forge questions are served on the git kind. ForgeRoomHeader carries the room the
// hello named.
const (
	ForgePrefix     = "/_forge/"
	ForgeRoomHeader = "X-Atrium-Forge-Room"
)

// SettingForgeEntries is the hub setting that names the forge per host, a JSON list of forge.Entry. Empty is the
// built-in table: github.com is GitHub and bitbucket.org is Bitbucket.
const SettingForgeEntries = "forge.entries"

// ForgeSettings is what the forge route needs of the hub's store. hubstore.Store has both.
type ForgeSettings interface {
	HubSetting(name string) (string, error)
	SetHubSetting(name, value string) error
}

type hubForge struct {
	st  ForgeSettings
	run forge.Runner
	// forgeOf, when set, picks the forge for a host instead of the entries. A seam for tests.
	forgeOf func(host string) (forge.Forge, error)

	mu sync.Mutex
	// open is the growler id of each raised alert, by `tool@host`.
	open map[string]string
}

// SetForge wires the hub's forge. run runs the forge CLIs and nil is forge.Exec. Without it the forge route answers
// 404, which a room reads as a hub that cannot be asked.
func (p *Proxy) SetForge(st ForgeSettings, run forge.Runner) {
	if run == nil {
		run = forge.Exec(hideWindow)
	}
	p.mu.Lock()
	p.fg = &hubForge{st: st, run: run, open: map[string]string{}}
	p.mu.Unlock()
	if p.hub != nil {
		p.hub.Forge = http.HandlerFunc(p.serveForge)
	}
}

func (p *Proxy) forgeSide() *hubForge {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fg
}

// entries is the hub's forge per host. A list that does not read is the built-in table, and the log says so.
func (f *hubForge) entries() []forge.Entry {
	raw, err := f.st.HubSetting(SettingForgeEntries)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []forge.Entry
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		log.Printf("[hub] %s does not read (%v), so the built-in forges are used", SettingForgeEntries, err)
		return nil
	}
	return out
}

func (f *hubForge) forgeFor(host string) (forge.Forge, error) {
	if f.forgeOf != nil {
		return f.forgeOf(host)
	}
	return forge.For(host, f.entries(), f.run)
}

// toolOf is the CLI a forge kind runs through, and the command it is called by on this hub.
func (f *hubForge) toolOf(kind, host string) (tool, cmd string) {
	tool = map[string]string{forge.GitHub: "gh", forge.Bitbucket: "bb", forge.GitLab: "glab"}[kind]
	cmd = tool
	if _, c, err := forge.Pick(host, f.entries()); err == nil && c != "" {
		cmd = c
	}
	return tool, cmd
}

// ── a room's question ───────────────────────────────────

func forgeFail(w http.ResponseWriter, code int, e *forge.HubError) {
	crJSON(w, code, e)
}

// validAsk is the shape check of a room's question. The names go into an argv and a store path, so they are held to
// what a forge allows.
var askPart = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

func validAsk(a forge.HubAsk, numbered bool) string {
	a.Host = strings.ToLower(strings.TrimSpace(a.Host))
	switch {
	case a.Host == "" || !forgeHost.MatchString(a.Host):
		return "say the forge host, a name like github.com"
	case !askPart.MatchString(a.Org) || !askPart.MatchString(a.Repo) || strings.HasPrefix(a.Org, "-") ||
		strings.HasPrefix(a.Repo, "-") || a.Org == "." || a.Org == ".." || a.Repo == "." || a.Repo == "..":
		return "say the owner and the repository"
	case numbered && a.Number <= 0:
		return "say the number"
	}
	return ""
}

var forgeHost = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?$`)

func (p *Proxy) serveForge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	f := p.forgeSide()
	if f == nil || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	room := strings.TrimSpace(r.Header.Get(ForgeRoomHeader))
	var ask forge.HubAsk
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&ask); err != nil {
		forgeFail(w, http.StatusBadRequest, &forge.HubError{Code: forge.CodeOther, Message: "that is not a forge question"})
		return
	}
	ask.Host = strings.ToLower(strings.TrimSpace(ask.Host))
	if why := validAsk(ask, r.URL.Path != forge.HubRepoPath); why != "" {
		forgeFail(w, http.StatusBadRequest, &forge.HubError{Code: forge.CodeOther, Message: why})
		return
	}
	var (
		out  any
		kind string
		err  error
	)
	switch r.URL.Path {
	case forge.HubPRPath:
		var ans forge.HubPR
		ans, err = p.forgePR(r.Context(), f, ask)
		out, kind = ans, ans.Kind
	case forge.HubIssuePath:
		var ans forge.HubIssue
		ans, err = p.forgeIssue(r.Context(), f, ask)
		out, kind = ans, ans.Kind
	case forge.HubRepoPath:
		var name string
		name, err = p.forgeHold(r.Context(), ask)
		out = forge.HubRepo{Store: name}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		code, he := p.forgeRefusal(f, room, ask, err)
		forgeFail(w, code, he)
		return
	}
	if kind != "" {
		f.worked(p, kind, ask.Host)
	}
	crJSON(w, http.StatusOK, out)
}

// forgeAccess is an access failure on the hub: the forge, which CLI, and why.
type forgeAccess struct {
	kind string
	err  error
}

func (e *forgeAccess) Error() string { return e.err.Error() }
func (e *forgeAccess) Unwrap() error { return e.err }

func (p *Proxy) forgePR(ctx context.Context, f *hubForge, ask forge.HubAsk) (forge.HubPR, error) {
	var ans forge.HubPR
	fg, err := f.forgeFor(ask.Host)
	if err != nil {
		return ans, err
	}
	ans.Kind = fg.Kind()
	ref := forge.Ref{Host: ask.Host, Org: ask.Org, Repo: ask.Repo, Number: ask.Number}
	wrap := func(err error) error { return &forgeAccess{kind: ans.Kind, err: err} }
	if ask.HeadOnly {
		head, err := fg.Head(ctx, ref)
		if err != nil {
			return ans, wrap(err)
		}
		ans.PR = &forge.PR{Head: head}
		return ans, nil
	}
	if ans.PR, err = fg.View(ctx, ref); err != nil {
		return ans, wrap(err)
	}
	ans.URL = fg.PRURL(ref)
	if ask.Diff {
		if ans.Diff, err = fg.Diff(ctx, ref); err != nil {
			return ans, wrap(err)
		}
	}
	if ask.Fetch {
		name, err := p.forgeHold(ctx, ask)
		if err != nil {
			return ans, err
		}
		g := p.git()
		spec := fg.FetchSpec(ref)
		_, cmd := f.toolOf(ans.Kind, ask.Host)
		helper := ""
		if ans.Kind == forge.GitHub {
			helper = "!" + cmd + " auth git-credential"
		}
		if _, err := g.Store().FetchPR(ctx, name, spec.Remote, spec.Refspec, forge.PRRef(ask.Number), ans.PR.BaseRef,
			helper); err != nil {
			return ans, wrap(err)
		}
		ans.Store, ans.Ref = name, forge.PRRef(ask.Number)
	}
	return ans, nil
}

func (p *Proxy) forgeIssue(ctx context.Context, f *hubForge, ask forge.HubAsk) (forge.HubIssue, error) {
	var ans forge.HubIssue
	fg, err := f.forgeFor(ask.Host)
	if err != nil {
		return ans, err
	}
	ans.Kind = fg.Kind()
	ir, ok := fg.(forge.IssueReader)
	if !ok {
		return ans, &forge.NoForgeError{Kind: ans.Kind}
	}
	ans.Issue, err = ir.Issue(ctx, forge.Ref{Host: ask.Host, Org: ask.Org, Repo: ask.Repo, Number: ask.Number})
	if err != nil {
		return ans, &forgeAccess{kind: ans.Kind, err: err}
	}
	return ans, nil
}

// forgeHold puts the repository in the hub's store, cloning it from the forge when the store does not hold it.
func (p *Proxy) forgeHold(ctx context.Context, ask forge.HubAsk) (string, error) {
	g := p.git()
	if g == nil {
		return "", errors.New("this hub has no git store, so it cannot hold the repository for a room")
	}
	return g.Store().Hold(ctx, "https://"+ask.Host+"/"+ask.Org+"/"+ask.Repo)
}

// forgeRefusal is the answer to a failed question, and raises the hub's alert when the failure is a login the hub is
// missing: a CLI that is not installed or not logged in, or a head fetch the forge refused a credential for.
func (p *Proxy) forgeRefusal(f *hubForge, room string, ask forge.HubAsk, err error) (int, *forge.HubError) {
	var (
		nf *forge.NoForgeError
		ae *forge.AccessError
		fe *gitsync.FetchError
		fa *forgeAccess
	)
	kind := ""
	if errors.As(err, &fa) {
		kind = fa.kind
	}
	switch {
	case errors.As(err, &nf):
		return http.StatusUnprocessableEntity, &forge.HubError{Code: forge.CodeNoForge, Message: nf.Error(),
			Host: ask.Host}
	case errors.As(err, &ae):
		tool, _ := f.toolOf(kind, ask.Host)
		if tool == "" {
			tool = ae.Tool
		}
		msg := f.raise(p, room, tool, ae.Tool, ask.Host, ae.NotInstalled, ae.Login)
		return http.StatusBadGateway, &forge.HubError{Code: forge.CodeAccess, Message: msg, Tool: tool, Host: ask.Host}
	case errors.As(err, &fe) && fe.Auth:
		tool, cmd := f.toolOf(kind, ask.Host)
		if tool == "" {
			return http.StatusBadGateway, &forge.HubError{Code: forge.CodeFetch, Message: fe.Error(), Host: ask.Host}
		}
		msg := f.raise(p, room, tool, cmd, ask.Host, false, "")
		return http.StatusBadGateway, &forge.HubError{Code: forge.CodeAccess, Message: fe.Error() + ". " + msg,
			Tool: tool, Host: ask.Host}
	case errors.As(err, &fe):
		return http.StatusBadGateway, &forge.HubError{Code: forge.CodeFetch, Message: fe.Error(), Host: ask.Host}
	}
	return http.StatusBadGateway, &forge.HubError{Code: forge.CodeOther, Message: "the hub: " + err.Error(),
		Host: ask.Host}
}

// ── the hub's alert ─────────────────────────────────────

// forgeSay is the sentence for a hub CLI that cannot be used, and the one command that fixes it. It names the hub,
// because a room shows it as it is and the login is the hub's.
func forgeSay(tool, cmd, host, state string, missing []string, login string) (message, fix string) {
	if login == "" {
		login = cmd + " auth login"
		if tool != "bb" {
			login += " --hostname " + host
		}
	}
	switch state {
	case forgeNotInstalled:
		return fmt.Sprintf("%s is not installed on the hub: install it on the hub, then run `%s` there", cmd, login), login
	case forgeMissingScope:
		fix = login
		if tool == "gh" {
			fix = fmt.Sprintf("%s auth refresh --hostname %s --scopes %s", cmd, host, strings.Join(missing, ","))
		}
		return fmt.Sprintf("%s on the hub is missing the scope %s for %s: run `%s` on the hub",
			cmd, strings.Join(missing, ", "), host, fix), fix
	}
	return fmt.Sprintf("%s is not logged in on the hub for %s: run `%s` on the hub", cmd, host, login), login
}

// forgeAlertSeq makes two raises in one clock tick differ, as the Windows clock is coarser than a raise is quick.
var forgeAlertSeq atomic.Uint64

// forgeAlertID is the growler of one open alert. A growler id is raised once ever, so each raise has its own.
func forgeAlertID(tool, host string, at time.Time) string {
	return "forge|" + tool + "@" + host + "|" + strconv.FormatInt(at.UnixNano(), 36) + "." +
		strconv.FormatUint(forgeAlertSeq.Add(1), 36)
}

func forgeAlertPrefix(tool, host string) string { return "forge|" + tool + "@" + host + "|" }

// raise raises the hub's alert for a CLI, hung on the room that asked, once while it is open. It answers the
// sentence the room is told.
func (f *hubForge) raise(p *Proxy, room, tool, cmd, host string, notInstalled bool, login string) string {
	state := forgeLoggedOut
	if notInstalled {
		state = forgeNotInstalled
	}
	msg, _ := forgeSay(tool, cmd, host, state, nil, login)
	g := p.growler()
	if g == nil || room == "" {
		return msg
	}
	rs, ok := g.st.(growlRaiser)
	if !ok {
		return msg
	}
	key := tool + "@" + host
	f.mu.Lock()
	defer f.mu.Unlock()
	if id := f.open[key]; id != "" || f.openIn(g, tool, host) != "" {
		return msg
	}
	id := forgeAlertID(tool, host, time.Now())
	raised, err := rs.Raise(room, GrowlRow{ID: id, Reason: ReasonQuestion, Subject: key,
		Title: "The hub cannot use " + cmd + " for " + host, Body: msg})
	if err != nil {
		log.Printf("[hub] growlers could not raise the forge alert for %s: %v", key, err)
		return msg
	}
	f.open[key] = id
	if raised {
		g.publish(nil)
	}
	p.RecordAudit(room, "forge-access", msg)
	return msg
}

// openIn finds an alert raised before this hub started, so a restart neither raises it again nor leaves it open
// after the login is fixed. Called with f.mu held.
func (f *hubForge) openIn(g *Growler, tool, host string) string {
	rows, err := g.st.Live()
	if err != nil {
		return ""
	}
	for _, r := range rows {
		if strings.HasPrefix(r.ID, forgeAlertPrefix(tool, host)) {
			f.open[tool+"@"+host] = r.ID
			return r.ID
		}
	}
	return ""
}

// worked ends the alert of a forge that just answered. Nothing open means nothing is said.
func (f *hubForge) worked(p *Proxy, kind, host string) {
	tool, _ := f.toolOf(kind, host)
	if tool == "" {
		return
	}
	f.end(p, tool, host)
}

func (f *hubForge) end(p *Proxy, tool, host string) {
	g := p.growler()
	if g == nil {
		return
	}
	key := tool + "@" + host
	f.mu.Lock()
	id := f.open[key]
	if id == "" {
		id = f.openIn(g, tool, host)
	}
	delete(f.open, key)
	f.mu.Unlock()
	if id != "" {
		g.endAbout(id)
	}
}

// ── the operator's routes ───────────────────────────────

const (
	forgeOK           = "ok"
	forgeNotInstalled = "not_installed"
	forgeLoggedOut    = "logged_out"
	forgeMissingScope = "missing_scope"
	forgeUnknown      = "unknown"
)

// forgeStatusArgs is the fixed status command per tool.
var forgeStatusArgs = map[string]func(host string) []string{
	"gh":   func(h string) []string { return []string{"auth", "status", "--hostname", h} },
	"glab": func(h string) []string { return []string{"auth", "status", "--hostname", h} },
	"bb":   func(h string) []string { return []string{"auth", "status"} },
}

var forgeDefaultHost = map[string]string{"gh": "github.com", "bb": "bitbucket.org", "glab": "gitlab.com"}

// ForgeStatus is one CLI's login on the hub.
type ForgeStatus struct {
	Tool    string   `json:"tool"`
	Host    string   `json:"host"`
	State   string   `json:"state"`
	Missing []string `json:"missing,omitempty"`
	Message string   `json:"message,omitempty"`
	Fix     string   `json:"fix,omitempty"`
}

var scopesLineRE = regexp.MustCompile(`(?i)token scopes?:\s*(.*)`)

// forgeScopesIn reads the scopes a status output says the login holds. ok is false when it says nothing about them,
// as a fine-grained token's does, in which case nothing can be said to be missing.
func forgeScopesIn(out string) (have map[string]bool, ok bool) {
	m := scopesLineRE.FindStringSubmatch(out)
	if m == nil {
		return nil, false
	}
	have = map[string]bool{}
	for _, f := range strings.FieldsFunc(m[1], func(r rune) bool {
		return r == ',' || r == ' ' || r == '\'' || r == '"' || r == '\r'
	}) {
		have[f] = true
	}
	return have, true
}

// check asks one CLI on the hub whether it is logged in to host. It is the CLI's own status command, never a token
// read. An ok ends the alert. A failure is answered and raises nothing, since no room is waiting on it.
func (f *hubForge) check(ctx context.Context, p *Proxy, tool, host string, scopes []string) ForgeStatus {
	st := ForgeStatus{Tool: tool, Host: host}
	argsFor, known := forgeStatusArgs[tool]
	if !known {
		st.State, st.Message = forgeUnknown, "unknown forge CLI, nothing was run. the ones there are: gh, bb, glab"
		return st
	}
	if host == "" {
		host = forgeDefaultHost[tool]
		st.Host = host
	}
	if !forgeHost.MatchString(strings.ToLower(host)) {
		st.State, st.Message = forgeUnknown, "that is not a host name, nothing was run"
		return st
	}
	kind := map[string]string{"gh": forge.GitHub, "bb": forge.Bitbucket, "glab": forge.GitLab}[tool]
	_, cmd := f.toolOf(kind, host)
	out, err := f.run(ctx, forge.Cmd{Name: cmd, Args: argsFor(host), Timeout: 20 * time.Second, Limit: 64 << 10})
	switch {
	case ctx.Err() != nil:
		st.State, st.Message = forgeUnknown, cmd+" did not answer in time"
		return st
	case err != nil && strings.Contains(err.Error(), "executable file not found"):
		st.State = forgeNotInstalled
	case err != nil:
		st.State = forgeLoggedOut
	}
	if st.State != "" {
		st.Message, st.Fix = forgeSay(tool, cmd, host, st.State, nil, "")
		return st
	}
	if have, ok := forgeScopesIn(string(out)); ok {
		for _, w := range scopes {
			if !have[w] {
				st.Missing = append(st.Missing, w)
			}
		}
	}
	if len(st.Missing) > 0 {
		st.State = forgeMissingScope
		st.Message, st.Fix = forgeSay(tool, cmd, host, st.State, st.Missing, "")
		return st
	}
	st.State = forgeOK
	f.end(p, tool, host)
	return st
}

// serveForgeAdmin is the operator's side: the entries per host, and the check of the hub's logins.
func (p *Proxy) serveForgeAdmin(w http.ResponseWriter, r *http.Request, rest string) {
	f := p.forgeSide()
	if f == nil {
		http.NotFound(w, r)
		return
	}
	switch {
	case rest == "" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		entries := f.entries()
		if entries == nil {
			entries = []forge.Entry{}
		}
		crJSON(w, http.StatusOK, map[string]any{"entries": entries, "defaults": forge.DefaultHosts})
	case rest == "" && r.Method == http.MethodPut:
		if err := docsCrossOrigin.Check(r); err != nil {
			crFail(w, http.StatusForbidden, "a page on another origin cannot change the hub's forges")
			return
		}
		var in struct {
			Entries []forge.Entry `json:"entries"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
			crFail(w, http.StatusBadRequest, "send {entries: [{host, forge, cmd}]}")
			return
		}
		for i, e := range in.Entries {
			e.Host = strings.ToLower(strings.TrimSpace(e.Host))
			e.Forge, e.Cmd = strings.TrimSpace(e.Forge), strings.TrimSpace(e.Cmd)
			switch {
			case !forgeHost.MatchString(e.Host):
				crFail(w, http.StatusBadRequest, fmt.Sprintf("%q is not a host name, a name like github.com", e.Host))
				return
			case e.Forge != "" && e.Forge != forge.GitHub && e.Forge != forge.Bitbucket && e.Forge != forge.GitLab &&
				e.Forge != forge.None:
				crFail(w, http.StatusBadRequest, "a forge is github, bitbucket, gitlab or none")
				return
			case e.Cmd != "" && !forgeCmd.MatchString(e.Cmd):
				crFail(w, http.StatusBadRequest, fmt.Sprintf("%q is not a command name. the hub stores a name found "+
					"on PATH, never a path or arguments", e.Cmd))
				return
			}
			in.Entries[i] = e
		}
		raw, _ := json.Marshal(in.Entries)
		if err := f.st.SetHubSetting(SettingForgeEntries, string(raw)); err != nil {
			crFail(w, http.StatusInternalServerError, err.Error())
			return
		}
		p.RecordAudit("", "forge-entries", string(raw))
		crJSON(w, http.StatusOK, map[string]any{"entries": in.Entries})
	case rest == "check" && r.Method == http.MethodPost:
		if err := docsCrossOrigin.Check(r); err != nil {
			crFail(w, http.StatusForbidden, "a page on another origin cannot run the hub's forge check")
			return
		}
		var in struct {
			Forges []struct {
				Tool   string   `json:"tool"`
				Host   string   `json:"host"`
				Scopes []string `json:"scopes"`
			} `json:"forges"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
			crFail(w, http.StatusBadRequest, "send {forges: [{tool, host, scopes}]}")
			return
		}
		if len(in.Forges) == 0 {
			for _, t := range []string{"gh", "bb"} {
				in.Forges = append(in.Forges, struct {
					Tool   string   `json:"tool"`
					Host   string   `json:"host"`
					Scopes []string `json:"scopes"`
				}{Tool: t})
			}
		}
		if len(in.Forges) > 8 {
			crFail(w, http.StatusBadRequest, "at most 8 forges in one check")
			return
		}
		out := make([]ForgeStatus, 0, len(in.Forges))
		for _, x := range in.Forges {
			out = append(out, f.check(r.Context(), p, strings.TrimSpace(x.Tool), strings.TrimSpace(x.Host), x.Scopes))
		}
		crJSON(w, http.StatusOK, map[string]any{"forges": out})
	default:
		crFail(w, http.StatusMethodNotAllowed, "that has to be a GET or PUT, or a POST to /check")
	}
}

var forgeCmd = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ── the room's side ─────────────────────────────────────

// Forge asks the hub a forge question over the git kind: in is posted to path and the answer decoded into out. A
// refusal comes back as a *forge.HubError with the hub's sentence. A hub that cannot be asked is an error that says
// so, and the room never falls back to a forge of its own.
func (r *Room) Forge(ctx context.Context, path string, in, out any) error {
	if !r.HubServesGit() {
		return errors.New("the hub cannot be asked about the forge right now: this room is not attached, or its hub " +
			"serves no git. a room reads pull requests and issues only through its hub")
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	r.mu.Lock()
	if r.claimRT == nil {
		r.claimRT = r.GitTransport()
	}
	rt := r.claimRT
	r.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://hub"+path, strings.NewReader(string(raw)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := rt.RoundTrip(req)
	if err != nil {
		return fmt.Errorf("the hub could not be asked about the forge: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		var he forge.HubError
		if json.Unmarshal(body, &he) == nil && he.Message != "" {
			return &he
		}
		if res.StatusCode == http.StatusNotFound {
			return errors.New("the hub does not run a forge for its rooms. it is older than this room, or has no " +
				"store: update the hub")
		}
		return fmt.Errorf("the hub answered %d to a forge question", res.StatusCode)
	}
	return json.Unmarshal(body, out)
}

// keep hubstore imported for the type of SetForge's usual argument.
var _ ForgeSettings = (*hubstore.Store)(nil)
