package api

import (
	"bufio"
	"encoding/json"
	"github.com/dovholuknf/atrium/internal/base/safepath"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The Claude sessions this machine already has, for the board's "adopt a session". `GET /v1/sessions/found` reads the
// newest transcript of every project folder under `~/.claude/projects` and changes nothing. `?dir=` looks in one
// folder instead, which is how a folder the first look missed is added.

// foundProjectCap and foundSessionCap bound the walk, since a machine that has used claude for a year has hundreds.
const (
	foundProjectCap = 400
	foundSessionCap = 12
)

type foundSession struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Cwd   string `json:"cwd"`
	At    string `json:"at"`
	Bytes int64  `json:"bytes"`
}

func (s *Server) sessionsFound(w http.ResponseWriter, r *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []foundSession{}})
		return
	}
	root := filepath.Join(home, ".claude", "projects")
	var dirs []string
	scan := ""
	if want := strings.TrimSpace(r.URL.Query().Get("dir")); want != "" {
		if strings.HasPrefix(want, "~/") {
			want = filepath.ToSlash(home) + want[1:]
		}
		want, err := safepath.LocalDir(want)
		if err != nil {
			writeErr(w, http.StatusBadRequest, errNoFolder(want))
			return
		}
		if st, err := os.Stat(want); err != nil || !st.IsDir() {
			writeErr(w, http.StatusBadRequest, errNoFolder(want))
			return
		}
		scan = want
		dirs = []string{filepath.Join(root, encodeProjectDir(want))}
	} else if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if e.IsDir() && len(dirs) < foundProjectCap {
				dirs = append(dirs, filepath.Join(root, e.Name()))
			}
		}
	}
	out := []foundSession{}
	for _, d := range dirs {
		out = append(out, newestSessionIn(d, scan)...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	if len(out) > foundSessionCap {
		out = out[:foundSessionCap]
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

type noFolder string

func (e noFolder) Error() string {
	return string(e) + " is not a folder on this machine. check the path and scan again"
}

func errNoFolder(p string) error { return noFolder(p) }

// newestSessionIn is the newest transcript in one project folder. With `dir` named it is the folder the person
// typed, and every transcript in it counts, up to the cap.
func newestSessionIn(projectDir, dir string) []foundSession {
	projectDir, err := safepath.LocalDir(projectDir)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil
	}
	type cand struct {
		name string
		at   time.Time
		size int64
	}
	var cands []cand
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if info, err := e.Info(); err == nil && info.Size() > 0 {
			cands = append(cands, cand{e.Name(), info.ModTime(), info.Size()})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].at.After(cands[j].at) })
	if dir == "" && len(cands) > 1 {
		cands = cands[:1]
	}
	if len(cands) > foundSessionCap {
		cands = cands[:foundSessionCap]
	}
	var out []foundSession
	for _, c := range cands {
		path := filepath.Join(projectDir, c.name)
		id := strings.TrimSuffix(c.name, ".jsonl")
		cwd := dir
		if cwd == "" {
			cwd = cwdOf(path)
		}
		if cwd == "" {
			continue
		}
		cwd, err := safepath.LocalDir(cwd)
		if err != nil {
			continue
		}
		if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
			continue
		}
		out = append(out, foundSession{ID: id, Title: titleOf(path, id), Cwd: filepath.ToSlash(cwd),
			At: c.at.UTC().Format(time.RFC3339), Bytes: c.size})
	}
	return out
}

// cwdOf is the folder a transcript was written in, which claude records on its records. The project folder's name
// cannot say, since every character that is not a letter or a digit became a dash.
func cwdOf(path string) string {
	path, err := safepath.LocalDir(path)
	if err != nil {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for i := 0; i < titleScanLines && sc.Scan(); i++ {
		var rec struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) == nil && rec.Cwd != "" {
			return rec.Cwd
		}
	}
	return ""
}
