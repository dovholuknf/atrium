package gitsync

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// SettingGitRoot is the room setting naming where clones live. Empty means ~/git. It is
// only ever set on the room, never by the hub.
const SettingGitRoot = "git_root"

// DefaultRoot is ~/git, where scripts/room-git.ps1 init already put clones.
func DefaultRoot() string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return ""
	}
	return filepath.Join(h, "git")
}

// RoomHandler is the room's git surface, mounted at /v1/git/:
//
//	POST /v1/git/sync          {name, init}, sent by the hub over the link
//	GET  /v1/git/status        the last answer of each sync, from memory
//	     /v1/git/<name>.git/   upload-pack over the clone, claude/* except claude/main
type RoomHandler struct {
	Syncer *Syncer
	// Hub reaches the hub for one sync. An error says why it cannot.
	Hub func() (http.RoundTripper, error)
}

// Handler builds the mux.
func (h *RoomHandler) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/git/sync", h.sync)
	mux.HandleFunc("GET /v1/git/status", h.status)
	mux.Handle("/v1/git/", &Backend{
		Prefix: "/v1/git",
		Resolve: func(name string) (string, bool) {
			root := h.Syncer.Root()
			if root == "" {
				return "", false
			}
			dir := filepath.Join(root, filepath.FromSlash(name), ".git")
			if st, err := os.Stat(dir); err != nil || !st.IsDir() {
				return "", false
			}
			return dir, true
		},
		// Everything, then claude/* back, then claude/main out again: the room offers what its
		// workers made and not what came from the hub in the first place.
		// HEAD too: it is not under refs, and the clone's HEAD may be any branch at all.
		Hide: []string{"HEAD", "refs","!refs/heads/claude/", "refs/heads/claude/main"},
	})
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *RoomHandler) sync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Init bool   `json:"init"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	// UNKNOWN FIELDS ARE REFUSED. The source branch is fixed, so a request that tries to name
	// one (a `branch`, a `ref`, a `url`) is refused rather than quietly ignored.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad sync request: " + err.Error()})
		return
	}
	// Not the request's context: a hub that gives up mid-fetch would kill git and leave locks.
	ctx, cancel := context.WithTimeout(context.Background(), CommandBound)
	defer cancel()
	writeJSON(w, http.StatusOK, h.Syncer.Sync(ctx, req.Name, req.Init, h.Hub))
}

func (h *RoomHandler) status(w http.ResponseWriter, r *http.Request) {
	all := h.Syncer.Last()
	if len(all) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"state": "none", "detail": "no sync has run since this room started", "repos": all,
			"now": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	// The top level is the worst answer, so a caller that reads one field is not told ok
	// while a repo is not.
	worst := all[0]
	rank := map[string]int{StateOK: 0, StateAbsent: 1, StateBehind: 2, StateFailed: 3}
	for _, s := range all[1:] {
		if rank[s.State] > rank[worst.State] {
			worst = s
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"state": worst.State, "sha": worst.SHA, "detail": worst.Detail, "name": worst.Name, "repos": all,
	})
}
