package claudeconf

import (
	"encoding/json"
	"os"
	"strings"
)

// The models the operator picks between, read from their own settings.
//
// ── the rule this does not break ─────────────────────────
//
// `discover.go` says it at length: ATRIUM DOES NOT KNOW WHICH MODELS EXIST and
// must never hold a list. They change every few months and one written into
// this repository ships wrong.
//
// This holds no list. It reads the one the OPERATOR wrote, in the file Claude
// Code already reads it from, and offers those names back. If they add a model
// tomorrow it appears; if they delete the section it goes away. The rule is
// about atrium having opinions about models, and this has none.
//
// ── why read it at all ───────────────────────────────────
//
// The launch dialog has a model box, and what somebody types into it is a
// model id: `claude-opus-4-7`, not `Opus 4.7`. Typing one correctly from memory
// to compare two models is the kind of task that quietly stops happening. The
// operator has already written the list down, once, with labels.
//
// ── the shape ────────────────────────────────────────────
//
//	"modelPicker": { "options": [ { "model": "...", "label": "..." } ] }
//
// Anything that is not that shape yields nothing, which reads as "no picker
// configured" and leaves the free text box exactly as it was.

// Model is one entry of the operator's picker.
type Model struct {
	// ID is what goes on the command line.
	ID string `json:"model"`
	// Label is what a human calls it. Empty falls back to the id, so a picker
	// written without labels still works.
	Label string `json:"label,omitempty"`
}

// Models reads the operator's model picker.
//
// A MISSING FILE IS NOT AN ERROR. Most machines have no picker configured and
// that is the normal case, not a fault to report on the board.
func Models() []Model {
	path, err := UserSettingsPath()
	if err != nil {
		return nil
	}
	return modelsIn(path)
}

func modelsIn(path string) []Model {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Picker struct {
			Options []Model `json:"options"`
		} `json:"modelPicker"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		// Their file, mid-edit or hand written. Nothing to offer and nothing
		// to complain about: Claude Code will tell them if it is broken, and
		// two programs reporting the same syntax error is noise.
		return nil
	}
	out := make([]Model, 0, len(doc.Picker.Options))
	seen := map[string]bool{}
	for _, m := range doc.Picker.Options {
		m.ID = strings.TrimSpace(m.ID)
		m.Label = strings.TrimSpace(m.Label)
		if m.ID == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	return out
}
