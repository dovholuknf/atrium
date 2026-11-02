package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/safepath"
)

// panelItem is one finding of merged.md.
type panelItem struct {
	Sev         string
	Num         int
	Category    string
	Path        string
	Line        int
	Text        string
	PreExisting bool
}

var (
	panelItemRe = regexp.MustCompile(`^- (high|medium|med|low|nit)-(\d+) (\S+) (\S+):(\d+)(?: \+ .*?)? -- (.*)$`)
	panelPreRe  = regexp.MustCompile(`^- pre-existing-(\d+) \((high|medium|med|low|nit)\) (\S+):(\d+) -- (.*)$`)
)

// parsePanel reads merged.md. A line that is neither a heading, a blank, a
// continuation nor an item is skipped and counted.
func parsePanel(text string) (items []panelItem, skipped int) {
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.HasPrefix(l, "  ") && len(items) > 0 {
			items[len(items)-1].Text += "\n" + strings.TrimSpace(l)
			continue
		}
		if m := panelItemRe.FindStringSubmatch(l); m != nil {
			n, _ := strconv.Atoi(m[2])
			ln, _ := strconv.Atoi(m[5])
			items = append(items, panelItem{Sev: panelSev(m[1]), Num: n, Category: m[3], Path: m[4], Line: ln, Text: m[6]})
		} else if m := panelPreRe.FindStringSubmatch(l); m != nil {
			n, _ := strconv.Atoi(m[1])
			ln, _ := strconv.Atoi(m[4])
			items = append(items, panelItem{Sev: panelSev(m[2]), Num: n, Path: m[3], Line: ln, Text: m[5], PreExisting: true})
		} else {
			skipped++
		}
	}
	return items, skipped
}

func panelSev(s string) string {
	if s == "medium" {
		return "med"
	}
	return s
}

// panelFile is the finding file an item is written as, numbered pos.
func panelFile(it panelItem, pos int) string {
	safe := regexp.MustCompile(`[^A-Za-z0-9._]+`).ReplaceAllString(it.Path, "-")
	return fmt.Sprintf("%02d-%s-%s-L%d.txt", pos, it.Sev, safe, it.Line)
}

func panelText(it panelItem, url string) string {
	id := fmt.Sprintf("panel-%s-%02d", it.Sev, it.Num)
	ev := "Group: panel\nCategory: " + it.Category + "\n"
	if it.PreExisting {
		id = fmt.Sprintf("panel-pre-%02d", it.Num)
		ev = "Group: panel\nPre-existing: yes\n"
	}
	first, _, _ := strings.Cut(it.Text, "\n")
	if len(first) > 120 {
		first = first[:120]
	}
	return fmt.Sprintf("%s\n%s %s line %d: %s\n\n%s\n\nEvidence\nId: %s\n%s",
		url, strings.ToUpper(it.Sev), it.Path, it.Line, first, it.Text, id, ev)
}

func panelPaths(dir string, number int) (merged, applied string, err error) {
	rel := ".review-panel/" + strconv.Itoa(number)
	if merged, err = safepath.Contained(dir, rel+"/merged.md"); err != nil {
		return "", "", err
	}
	applied, err = safepath.Contained(dir, rel+"/applied.txt")
	return merged, applied, err
}

// GET /v1/prs/{id}/panel
func (s *Server) getPRPanel(w http.ResponseWriter, r *http.Request) {
	p, dir, ok := s.prForDrawer(w, r)
	if !ok {
		return
	}
	merged, applied, err := panelPaths(dir, p.Number)
	if err != nil {
		prOutside(w)
		return
	}
	raw, err := os.ReadFile(merged)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"found": false})
		return
	}
	items, skipped := parsePanel(string(raw))
	hash := sha256Hex(raw)
	done, _ := os.ReadFile(applied)
	mode := ""
	if h, m, ok := strings.Cut(strings.TrimSpace(string(done)), " "); ok && h == hash {
		mode = m
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": true, "count": len(items), "skipped": skipped,
		"hash": hash, "applied": mode})
}

// POST /v1/prs/{id}/panel
func (s *Server) applyPRPanel(w http.ResponseWriter, r *http.Request) {
	p, dir, ok := s.prForDrawer(w, r)
	if !ok {
		return
	}
	var body struct {
		Mode string `json:"mode"`
		Hash string `json:"hash"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		prError(w, http.StatusBadRequest, "bad_request", "the body is not json: "+err.Error(), nil)
		return
	}
	if body.Mode != "replace" && body.Mode != "merge" && body.Mode != "both" {
		prError(w, http.StatusBadRequest, "bad_request", "mode is replace, merge or both", nil)
		return
	}
	merged, applied, err := panelPaths(dir, p.Number)
	if err != nil {
		prOutside(w)
		return
	}
	raw, err := os.ReadFile(merged)
	if err != nil {
		prError(w, http.StatusNotFound, "not_found", "no merged.md for this review", nil)
		return
	}
	hash := sha256Hex(raw)
	if body.Hash != hash {
		prError(w, http.StatusConflict, "changed", "merged.md changed since it was read", map[string]any{"hash": hash})
		return
	}
	items, skipped := parsePanel(string(raw))
	prWalkMu.Lock()
	defer prWalkMu.Unlock()
	have, err := loadFindings(dir)
	if err != nil {
		prOutside(w)
		return
	}
	fdir, err := safepath.Contained(dir, "findings")
	if err != nil {
		prOutside(w)
		return
	}
	pos := 0
	if body.Mode == "replace" {
		old := fmt.Sprintf("%s.before-panel-%d", fdir, time.Now().Unix())
		if err := os.Rename(fdir, old); err != nil {
			prError(w, http.StatusInternalServerError, "write_failed", err.Error(), nil)
			return
		}
		if err := os.MkdirAll(fdir, 0o755); err != nil {
			prError(w, http.StatusInternalServerError, "write_failed", err.Error(), nil)
			return
		}
		have = nil
	}
	seen := map[string]bool{}
	for _, f := range have {
		seen[f.Path+":"+strconv.Itoa(f.Line)] = true
		pos = max(pos, f.Position)
	}
	added := 0
	for _, it := range items {
		if body.Mode == "merge" && seen[it.Path+":"+strconv.Itoa(it.Line)] {
			continue
		}
		pos++
		if err := os.WriteFile(filepath.Join(fdir, panelFile(it, pos)), []byte(panelText(it, p.URL)), 0o644); err != nil {
			prError(w, http.StatusInternalServerError, "write_failed", err.Error(), nil)
			return
		}
		added++
	}
	if err := os.WriteFile(applied, []byte(hash+" "+body.Mode+"\n"), 0o644); err != nil {
		prError(w, http.StatusInternalServerError, "write_failed", err.Error(), nil)
		return
	}
	s.PublishPR(p.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "added": added, "skipped": skipped})
}
