package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/safepath"
)

// Which of these words are actually files.
//
// The board underlines a path in a terminal so it can be clicked open, and the
// only hard part of that is NOT UNDERLINING EVERYTHING. A terminal is full of
// things shaped like a filename and not one: `v2.1.263`, `zrok.io`, `foo.bar()`,
// `Opus 5`. Every rule that tells them apart by looking is wrong somewhere, and
// wrong here means a page of noise with underlines under half the words.
//
// So the board does not decide. It finds candidates, sends them here, and this
// answers which ones exist. The rule becomes "it is a link if it is a file",
// which is the rule anybody would have asked for.
//
// A BATCH, because the alternative is one request per word on a line and the
// board asks on hover. Bounded on both axes: the batch and each path.
//
// IT NEVER LEAVES THE CARD, like every other file endpoint here. A path that
// resolves outside the worktree is simply absent from the answer, which is the
// same thing this says about a path that is not there, so it reports no more
// about the machine than `files/list` already does.

// maxProbe is how many candidates one request may carry. A terminal line at
// any sane width cannot hold more path-shaped words than this.
const maxProbe = 64

// maxProbePath is the longest candidate worth asking about. Past this it is
// not a path somebody is going to click, it is a line of output.
const maxProbePath = 512

// probeHit is one candidate that turned out to be real.
type probeHit struct {
	// Path is the candidate exactly as the board sent it, so the answer can be
	// matched back to the token on screen without re-deriving anything.
	Path string `json:"path"`
	// Rel is where it landed inside the card, which is what every other file
	// endpoint wants and what the board shows on hover.
	Rel string `json:"rel"`
	// Dir separates "open this in the editor" from "show me this directory",
	// which are different clicks.
	Dir bool `json:"dir"`
	// Size, for the hover. Zero for a directory.
	Size int64 `json:"size,omitempty"`
}

func (s *Server) probeFiles(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if strings.TrimSpace(task.Worktree) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("this card has no directory"))
		return
	}

	var body struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(body.Paths) > maxProbe {
		body.Paths = body.Paths[:maxProbe]
	}

	root := filepath.FromSlash(task.Worktree)
	hits := make([]probeHit, 0, len(body.Paths))
	seen := map[string]bool{}
	for _, want := range body.Paths {
		want = strings.TrimSpace(want)
		if want == "" || len(want) > maxProbePath || seen[want] {
			continue
		}
		seen[want] = true
		hit, ok := probeOne(root, want)
		if !ok {
			continue
		}
		hits = append(hits, hit)
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": hits})
}

// probeOne answers for a single candidate.
//
// An absolute path is allowed IF IT LANDS INSIDE THE CARD, which is the common
// case worth supporting: a runner prints the full path far more often than a
// relative one. `safepath.Contained` is what decides, and it follows symlinks
// on both sides, so a link pointing out of the worktree is refused the same as
// a `..` would be.
func probeOne(root, want string) (probeHit, bool) {
	full, err := safepath.Contained(root, want)
	if err != nil {
		return probeHit{}, false
	}
	fi, err := os.Lstat(full)
	if err != nil {
		return probeHit{}, false
	}
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return probeHit{}, false
	}
	out := probeHit{Path: want, Rel: filepath.ToSlash(rel), Dir: fi.IsDir()}
	if !fi.IsDir() {
		out.Size = fi.Size()
	}
	return out, true
}
