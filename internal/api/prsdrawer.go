package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/prreview/render"
	"github.com/dovholuknf/atrium/internal/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// The drawer routes: reading a review's findings, editing one, marking the walk,
// and launching the walker. docs/rnd/pulls-api.md is the contract.
//
// Every file is reached through internal/safepath against the row's run folder,
// and the run folder itself has to be inside the reviews root. No request carries
// a path: a finding is named by its key and the daemon maps it to a file.

// maxFindingBytes bounds a finding write.
const maxFindingBytes = 2 << 20

// prWalkMu serialises writes to walk.txt. A mark replaces one line and nothing
// else, so two marks for different findings must not lose each other.
var prWalkMu sync.Mutex

func prOutside(w http.ResponseWriter) {
	prError(w, http.StatusForbidden, "outside", "outside the review folder", nil)
}

// prFolder resolves a row's run folder through safepath. ok is false when the
// answer has already been written.
func (s *Server) prFolder(w http.ResponseWriter, p *store.PRReview) (string, bool) {
	root := filepath.FromSlash(s.st.ReviewsRoot())
	if p.RunDir == "" || strings.TrimSpace(root) == "" {
		prOutside(w)
		return "", false
	}
	dir, err := safepath.Contained(root, filepath.FromSlash(p.RunDir))
	if err != nil {
		prOutside(w)
		return "", false
	}
	return dir, true
}

// PRFinding is one finding file, parsed.
type PRFinding struct {
	Key      string `json:"key"`
	Position int    `json:"position"`
	File     string `json:"file"`
	Sev      string `json:"sev"`
	// Blocking is true when the finding is named and labelled BLOCKING. Its sev is
	// `high`. Additive: a client that does not know it loses nothing.
	Blocking bool              `json:"blocking"`
	Path     string            `json:"path"`
	Line     int               `json:"line"`
	Code     string            `json:"code"`
	Link     string            `json:"link"`
	Comment  string            `json:"comment"`
	Evidence map[string]string `json:"evidence"`
	Leak     string            `json:"leak"`
	Hunk     string            `json:"hunk"`
	Walk     PRFindingWalk     `json:"walk"`
	Text     string            `json:"text"`
	Hash     string            `json:"hash"`
}

// PRFindingWalk is a finding's line in walk.txt.
type PRFindingWalk struct {
	State string `json:"state"`
	At    string `json:"at"`
	URL   string `json:"url"`
}

var labelLine = regexp.MustCompile(`^(?i:high|blocking|medium|med|low|nit)\s+(\S+)\s+line\s+(\d+):\s*(.*)$`)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// parseFinding reads the shape review-tab-design 1.2 describes: the PR url, the
// label line, the deep link, the bullets, then Evidence. Anything it cannot find
// is left empty, since a hand-written file is still a finding.
func parseFinding(name string, pos int, sev string, raw []byte) PRFinding {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	f := PRFinding{File: name, Position: pos, Sev: sev, Evidence: map[string]string{},
		Text: text, Hash: sha256Hex(raw), Walk: PRFindingWalk{State: "open"}}
	lines := strings.Split(text, "\n")
	i := 0
	if i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "http") {
		i++
	}
	if i < len(lines) {
		if m := labelLine.FindStringSubmatch(strings.TrimSpace(lines[i])); m != nil {
			f.Path = m[1]
			f.Line, _ = strconv.Atoi(m[2])
			f.Code = strings.TrimSpace(m[3])
			i++
		}
	}
	if i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "http") {
		f.Link = strings.TrimSpace(lines[i])
		i++
	}
	var comment []string
	for ; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "Evidence" {
			i++
			break
		}
		comment = append(comment, lines[i])
	}
	f.Comment = strings.TrimSpace(strings.Join(comment, "\n"))
	for ; i < len(lines); i++ {
		k, v, ok := strings.Cut(lines[i], ":")
		if !ok || strings.TrimSpace(k) == "" {
			continue
		}
		f.Evidence[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	f.Leak = f.Evidence["Leak"]
	if id := f.Evidence["Id"]; id != "" {
		f.Key = "f-" + id
	} else {
		f.Key = "f-" + sha256Hex([]byte(f.Path + "\n" + f.Code))[:10]
	}
	return f
}

// hunkAround returns the lines of pr.diff around line in path, or "".
func hunkAround(diff, path string, line int) string {
	if diff == "" || path == "" || line <= 0 {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(diff, "\r\n", "\n"), "\n")
	inFile := false
	hunkRe := regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if strings.HasPrefix(l, "diff --git ") {
			inFile = strings.HasSuffix(l, " b/"+path)
			continue
		}
		if !inFile {
			continue
		}
		m := hunkRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		start, _ := strconv.Atoi(m[1])
		n := 1
		if m[2] != "" {
			n, _ = strconv.Atoi(m[2])
		}
		if line < start || line >= start+max(n, 1) {
			continue
		}
		end := i + 1
		for end < len(lines) && !strings.HasPrefix(lines[end], "@@") && !strings.HasPrefix(lines[end], "diff --git ") {
			end++
		}
		body := lines[i+1 : end]
		// Keep a window of about 12 lines each way of the new line number.
		cur, from, to := start, 0, len(body)
		for j, b := range body {
			if strings.HasPrefix(b, "-") {
				continue
			}
			if cur == line-12 && from == 0 {
				from = j
			}
			if cur == line+12 {
				to = j + 1
				break
			}
			cur++
		}
		return strings.TrimRight(l+"\n"+strings.Join(body[from:to], "\n"), "\n")
	}
	return ""
}

// loadFindings reads every finding of a run folder, in walk order.
func loadFindings(dir string) ([]PRFinding, error) {
	fdir, err := safepath.Contained(dir, "findings")
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(fdir)
	if err != nil {
		return nil, err
	}
	var diff string
	if p, err := safepath.Contained(dir, "pr.diff"); err == nil {
		if b, err := os.ReadFile(p); err == nil {
			diff = string(b)
		}
	}
	walk := readWalk(dir)
	var out []PRFinding
	for _, e := range ents {
		pos, sev, ok := findingSev(e.Name())
		if !ok || e.IsDir() {
			continue
		}
		full, err := safepath.Contained(dir, filepath.Join("findings", e.Name()))
		if err != nil {
			// A symlink out of the folder is skipped rather than followed.
			continue
		}
		raw, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		f := parseFinding(e.Name(), pos, sev, raw)
		f.Blocking = findingBlocking(e.Name())
		f.Hunk = hunkAround(diff, f.Path, f.Line)
		if wl, ok := walk[e.Name()]; ok {
			f.Walk = PRFindingWalk{State: wl.State, At: wl.At, URL: wl.URL}
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].File < out[j].File
	})
	return out, nil
}

// findingsETag changes whenever a finding file or walk.txt does.
func findingsETag(dir string) string {
	h := sha256.New()
	ents, _ := os.ReadDir(filepath.Join(dir, "findings"))
	for _, e := range ents {
		if fi, err := e.Info(); err == nil {
			fmt.Fprintf(h, "%s %d %d\n", e.Name(), fi.ModTime().UnixNano(), fi.Size())
		}
	}
	if fi, err := os.Stat(filepath.Join(dir, "walk.txt")); err == nil {
		fmt.Fprintf(h, "walk %d %d\n", fi.ModTime().UnixNano(), fi.Size())
	}
	return `"` + hex.EncodeToString(h.Sum(nil))[:24] + `"`
}

// prForDrawer looks the row up and resolves its folder. ok is false when the
// answer is already written.
func (s *Server) prForDrawer(w http.ResponseWriter, r *http.Request) (*store.PRReview, string, bool) {
	p, err := s.st.PRByID(r.PathValue("id"))
	if err != nil {
		s.prFail(w, err)
		return nil, "", false
	}
	dir, ok := s.prFolder(w, p)
	if !ok {
		return nil, "", false
	}
	return p, dir, true
}

// GET /v1/prs/{id}/findings
func (s *Server) getPRFindings(w http.ResponseWriter, r *http.Request) {
	p, dir, ok := s.prForDrawer(w, r)
	if !ok {
		return
	}
	if p.State != store.PRReady {
		prError(w, http.StatusConflict, "not_ready", "the review is not ready", map[string]any{"state": p.State})
		return
	}
	etag := findingsETag(dir)
	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	fs, err := loadFindings(dir)
	if err != nil {
		prOutside(w)
		return
	}
	if fs == nil {
		fs = []PRFinding{}
	}
	w.Header().Set("ETag", etag)
	writeJSON(w, http.StatusOK, map[string]any{"pr": viewPR(p), "findings": fs})
}

func findByKey(fs []PRFinding, key string) *PRFinding {
	for i := range fs {
		if fs[i].Key == key {
			return &fs[i]
		}
	}
	return nil
}

// PUT /v1/prs/{id}/findings/{key}
func (s *Server) putPRFinding(w http.ResponseWriter, r *http.Request) {
	p, dir, ok := s.prForDrawer(w, r)
	if !ok {
		return
	}
	var body textIn
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxFindingBytes+(1<<16))).Decode(&body); err != nil {
		prError(w, http.StatusBadRequest, "bad_request", "could not read that request", nil)
		return
	}
	if strings.TrimSpace(body.Hash) == "" {
		prError(w, http.StatusBadRequest, "bad_request", "a write has to say what it was based on", nil)
		return
	}
	if len(body.Text) > maxFindingBytes {
		prError(w, http.StatusBadRequest, "bad_request", "that finding is too large", nil)
		return
	}
	prWalkMu.Lock()
	defer prWalkMu.Unlock()
	fs, err := loadFindings(dir)
	if err != nil {
		prOutside(w)
		return
	}
	f := findByKey(fs, r.PathValue("key"))
	if f == nil {
		prError(w, http.StatusNotFound, "not_found", "no such finding", nil)
		return
	}
	full, err := safepath.Contained(dir, filepath.Join("findings", f.File))
	if err != nil {
		prOutside(w)
		return
	}
	current, err := os.ReadFile(full)
	if err != nil {
		prOutside(w)
		return
	}
	if got := sha256Hex(current); got != body.Hash {
		prError(w, http.StatusConflict, "changed",
			"that file changed while you were editing it. nothing was written.",
			map[string]any{"text": strings.ReplaceAll(string(current), "\r\n", "\n"), "hash": got})
		return
	}
	out := strings.ReplaceAll(body.Text, "\r\n", "\n")
	if body.Eol == "\r\n" {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	if err := os.WriteFile(full, []byte(out), 0o644); err != nil {
		prError(w, http.StatusInternalServerError, "write_failed", err.Error(), nil)
		return
	}
	key := parseFinding(f.File, f.Position, f.Sev, []byte(out)).Key
	s.PublishPR(p.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "hash": sha256Hex([]byte(out)), "key": key})
}

// POST /v1/prs/{id}/findings/{key}/walk
func (s *Server) walkPRFinding(w http.ResponseWriter, r *http.Request) {
	p, dir, ok := s.prForDrawer(w, r)
	if !ok {
		return
	}
	var body struct {
		State string `json:"state"`
		URL   string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		prError(w, http.StatusBadRequest, "bad_request", "the body is not json: "+err.Error(), nil)
		return
	}
	switch body.State {
	case "done", "accepted", "deferred", "skipped", "open":
	default:
		prError(w, http.StatusBadRequest, "bad_request", "state is open, accepted, done, skipped or deferred", nil)
		return
	}
	link := strings.TrimSpace(body.URL)
	if body.State != "done" {
		link = ""
	}
	if link != "" {
		u, err := url.Parse(link)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			len(link) >= 2000 || strings.ContainsAny(link, " \t\r\n") {
			prError(w, http.StatusBadRequest, "bad_url", "url has to be an http or https link under 2000 characters", nil)
			return
		}
	}
	prWalkMu.Lock()
	defer prWalkMu.Unlock()
	fs, err := loadFindings(dir)
	if err != nil {
		prOutside(w)
		return
	}
	f := findByKey(fs, r.PathValue("key"))
	if f == nil {
		prError(w, http.StatusNotFound, "not_found", "no such finding", nil)
		return
	}
	walkPath, err := safepath.Contained(dir, "walk.txt")
	if err != nil {
		prOutside(w)
		return
	}
	line := f.File + "  " + body.State
	at := ""
	if body.State != "open" {
		at = time.Now().UTC().Format("2006-01-02T15:04:05Z")
		line += "  " + at
		if link != "" {
			line += "  " + link
		}
	}
	if err := replaceWalkLine(walkPath, f.File, line); err != nil {
		prError(w, http.StatusInternalServerError, "write_failed", err.Error(), nil)
		return
	}
	s.PublishPR(p.ID)
	_, counts := readPRCounts(dir)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true,
		"walk": PRFindingWalk{State: body.State, At: at, URL: link}, "counts": counts})
}

// replaceWalkLine rewrites walk.txt with one finding's line replaced, or added.
// Every other line is kept byte for byte, so a line a walker wrote in its own
// shape survives.
func replaceWalkLine(path, file, line string) error {
	var lines []string
	if raw, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimRight(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n"), "\n")
	} else if !os.IsNotExist(err) {
		return err
	}
	replaced := false
	out := lines[:0:0]
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if name, _, _, ok := render.WalkLine(l); ok && name == file {
			if !replaced {
				out = append(out, line)
				replaced = true
			}
			continue
		}
		out = append(out, l)
	}
	if !replaced {
		out = append(out, line)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(out, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// POST /v1/prs/{id}/walker
func (s *Server) walkerPR(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.PRByID(r.PathValue("id"))
	if err != nil {
		s.prFail(w, err)
		return
	}
	var body struct {
		Action string `json:"action"`
		Task   string `json:"task"`
	}
	if raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16)); strings.TrimSpace(string(raw)) != "" {
		if err := json.Unmarshal(raw, &body); err != nil {
			prError(w, http.StatusBadRequest, "bad_request", "the body is not json: "+err.Error(), nil)
			return
		}
	}
	switch body.Action {
	case "", "launch":
		s.launchWalker(w, p)
	case "set":
		if strings.TrimSpace(body.Task) == "" {
			prError(w, http.StatusBadRequest, "bad_request", "set needs a task", nil)
			return
		}
		if _, err := s.st.SetPRWalker(p.ID, strings.TrimSpace(body.Task)); err != nil {
			s.prFail(w, err)
			return
		}
		s.prAnswer(w, http.StatusOK, p.ID, nil)
	case "clear":
		if _, err := s.st.SetPRWalker(p.ID, ""); err != nil {
			s.prFail(w, err)
			return
		}
		s.prAnswer(w, http.StatusOK, p.ID, nil)
	default:
		prError(w, http.StatusBadRequest, "bad_request", "action is launch, set or clear", nil)
	}
}

func (s *Server) launchWalker(w http.ResponseWriter, p *store.PRReview) {
	if s.Launch == nil {
		prError(w, http.StatusNotImplemented, "not_wired", "no launcher wired", nil)
		return
	}
	if p.State != store.PRReady {
		prError(w, http.StatusConflict, "not_ready", "the review is not ready", map[string]any{"state": p.State})
		return
	}
	if p.WalkerTask != "" {
		if t, err := s.st.Get(p.WalkerTask); err == nil && t.Status != store.StatusDone && t.Status != store.StatusDead {
			s.prAnswer(w, http.StatusOK, p.ID, map[string]any{"task": t.ID, "launched": false})
			return
		}
	}
	dir, ok := s.prFolder(w, p)
	if !ok {
		return
	}
	rec, err := s.st.RecipeFor(p.OrgRepo)
	if err != nil {
		prError(w, http.StatusInternalServerError, "launch_failed", err.Error(), nil)
		return
	}
	req, _ := json.Marshal(map[string]any{
		"harness": rec.Harness,
		"cwd":     filepath.ToSlash(dir),
		"title":   fmt.Sprintf("walk %s#%d", p.OrgRepo, p.Number),
		"prompt":  strings.ReplaceAll(rec.WalkerBrief, "<n>", strconv.Itoa(p.Number)),
		"url":     p.URL,
		"tags": []string{"atrium:subagent", "dept:review", "review", "pr",
			fmt.Sprintf("pr:%s#%d", p.OrgRepo, p.Number)},
	})
	t, err := s.Launch(req)
	if err != nil {
		prError(w, http.StatusInternalServerError, "launch_failed", err.Error(), nil)
		return
	}
	if _, err := s.st.SetPRWalker(p.ID, t.ID); err != nil {
		s.prFail(w, err)
		return
	}
	s.prAnswer(w, http.StatusCreated, p.ID, map[string]any{"task": t.ID, "launched": true})
}
