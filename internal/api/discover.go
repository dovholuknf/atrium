package api

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// Finding out which runners this machine actually has.
//
// A runner is a row in a table, which means enabling one is filling in a
// command line. Nothing checked whether that command existed, so a runner could
// look configured and fail on first use, and a runner you already have
// installed still had to be typed in from memory.
//
// Both answers come from PATH. `exec.LookPath` is the same lookup the daemon
// does when it starts a runner, so what discovery reports is what launching
// will find.

// harnessView is a configured runner plus whether its command is really there.
type harnessView struct {
	*store.Harness
	// Found is the resolved path of Cmd, or "" when it is not on PATH.
	Found string `json:"found"`
}

// candidate is a runner this machine has that is not configured to use it.
type candidate struct {
	// ID and Label are what the row would be called.
	ID    string `json:"id"`
	Label string `json:"label"`
	Cmd   string `json:"cmd"`
	Found string `json:"found"`
	// Args and ResumeArgs are filled in only where the invocation is known.
	// A guessed flag produces a runner that fails on first use and looks like
	// atrium is broken, so unknown is left empty and said out loud.
	Args       []string `json:"args"`
	ResumeArgs []string `json:"resume_args"`
	// Confirm is what still needs a human, or "" when nothing does.
	Confirm string `json:"confirm"`
}

// knownRunners are the command names worth probing for.
//
// The list is short on purpose. Probing PATH for everything an agent could
// possibly be would turn a helpful answer into a list to read.
var knownRunners = []candidate{
	{
		ID: "claude", Label: "claude code", Cmd: "claude",
		ResumeArgs: []string{"--resume", "{resume}"},
	},
	{
		ID: "codex", Label: "codex", Cmd: "codex",
		Confirm: "codex takes no arguments to open interactively. its resume flag is not " +
			"known here, so picking a conversation back up needs that filled in.",
	},
	{
		ID: "ollama", Label: "ollama", Cmd: "ollama",
		Args: []string{"run"},
		Confirm: "ollama needs a model after run, and has no concept of resuming a " +
			"conversation, so a shelved ollama card starts fresh.",
	},
	{
		ID: "aider", Label: "aider", Cmd: "aider",
		Confirm: "aider's arguments depend on how you use it, so nothing is assumed.",
	},
	{
		ID: "shell", Label: "a shell", Cmd: shellName(),
		Confirm: "a bare shell reports nothing about itself, so its card shows the " +
			"terminal and nothing else.",
	},
}

// LookPath resolves a command the same way launching will, and reports the
// path so a caller can show what would actually run. Empty when it is not
// there.
func LookPath(cmd string) string {
	if strings.TrimSpace(cmd) == "" {
		return ""
	}
	p, err := exec.LookPath(cmd)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(p)
}

// listHarnesses returns the configured runners, each saying whether its command
// exists on this machine.
func (s *Server) listHarnesses(w http.ResponseWriter, r *http.Request) {
	hs, err := s.st.Harnesses()
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]harnessView, 0, len(hs))
	for _, h := range hs {
		out = append(out, harnessView{Harness: h, Found: LookPath(h.Cmd)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"harnesses": out})
}

// discoverRunners reports runners this machine has that are not set up yet.
//
// Only ones actually on PATH, and only ones without a row already, so the
// answer is a short list of things you could turn on rather than a catalogue.
func (s *Server) discoverRunners(w http.ResponseWriter, r *http.Request) {
	hs, err := s.st.Harnesses()
	if err != nil {
		s.fail(w, err)
		return
	}
	// A row already exists for these, so discovery has nothing to offer.
	// Matched on the command as well as the id, since a row called something
	// else may already run the same binary.
	haveID := map[string]bool{}
	haveCmd := map[string]bool{}
	for _, h := range hs {
		haveID[strings.ToLower(h.ID)] = true
		if h.Cmd != "" {
			haveCmd[strings.ToLower(filepath.Base(h.Cmd))] = true
		}
	}

	found := make([]candidate, 0, len(knownRunners))
	for _, c := range knownRunners {
		if haveID[strings.ToLower(c.ID)] || haveCmd[strings.ToLower(c.Cmd)] {
			continue
		}
		if p := LookPath(c.Cmd); p != "" {
			c.Found = p
			found = append(found, c)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": found})
}
