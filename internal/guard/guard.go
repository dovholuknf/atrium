// Package guard is the PreToolUse policy that used to live in a PowerShell
// script in dotfiles: which commands, edits and subagents a session may run.
//
// The rules are data (rules.json, embedded). Each names a check, which is Go
// code here, and carries its own parameters and the sentence the model reads
// when it is refused. Commands are parsed by a real shell parser, mvdan.cc/sh
// for the Bash tool and a tokenizer for the PowerShell tool, so a rule sees
// commands and arguments rather than text. See
// docs/changes/r-hooks-all-in-atrium.md.
package guard

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed rules.json
var builtinRules []byte

// Rule is one entry of the rule data.
type Rule struct {
	ID    string   `json:"id"`
	Check string   `json:"check"`
	Tools []string `json:"tools"`
	// Decision is "deny" (the default) or "allow". An allow answers the call
	// without the permission prompt and stops the rules after it.
	Decision string `json:"decision,omitempty"`
	// Reason is what the model reads. `{name}` is replaced by what the check
	// found: the branch, the path, the git word.
	Reason string `json:"reason"`
	Params Params `json:"params"`

	patterns []*regexp.Regexp
}

// Params are a check's settings. Each check reads the ones it needs.
type Params struct {
	Names    []string `json:"names,omitempty"`
	Phrase   string   `json:"phrase,omitempty"`
	Except   []string `json:"except,omitempty"`
	Prefix   string   `json:"prefix,omitempty"`
	Verbs    []string `json:"verbs,omitempty"`
	Remotes  []string `json:"remotes,omitempty"`
	Options  []string `json:"options,omitempty"`
	Patterns []string `json:"patterns,omitempty"`
	Contains string   `json:"contains,omitempty"`
	Env      string   `json:"env,omitempty"`
	Allow    []string `json:"allow,omitempty"`
	Dir      string   `json:"dir,omitempty"`
}

// Rules is a loaded, checked rule set.
type Rules struct {
	List []*Rule
}

// Builtin is the rule set compiled into atrium. It is checked by a test, so a
// build that ships with it broken does not pass.
func Builtin() (*Rules, error) {
	return Load(builtinRules)
}

// Load reads and checks rule data. Every problem is an error: a rule set the
// guard only half understands is not loaded at all. See the failure posture in
// docs/changes/r-hooks-all-in-atrium.md.
func Load(raw []byte) (*Rules, error) {
	var doc struct {
		Rules []*Rule `json:"rules"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("the rules do not parse: %w", err)
	}
	if len(doc.Rules) == 0 {
		return nil, fmt.Errorf("there are no rules")
	}
	seen := map[string]bool{}
	for i, r := range doc.Rules {
		if r == nil || r.ID == "" {
			return nil, fmt.Errorf("rule %d has no id", i+1)
		}
		if seen[r.ID] {
			return nil, fmt.Errorf("rule %s appears twice", r.ID)
		}
		seen[r.ID] = true
		if _, ok := checks[r.Check]; !ok {
			return nil, fmt.Errorf("rule %s: there is no check called %q", r.ID, r.Check)
		}
		if len(r.Tools) == 0 {
			return nil, fmt.Errorf("rule %s names no tools", r.ID)
		}
		if strings.TrimSpace(r.Reason) == "" {
			return nil, fmt.Errorf("rule %s has no reason", r.ID)
		}
		switch r.Decision {
		case "":
			r.Decision = "deny"
		case "deny", "allow":
		default:
			return nil, fmt.Errorf("rule %s: decision %q is neither deny nor allow", r.ID, r.Decision)
		}
		for _, p := range r.Params.Patterns {
			re, err := regexp.Compile(p)
			if err != nil {
				return nil, fmt.Errorf("rule %s: %w", r.ID, err)
			}
			r.patterns = append(r.patterns, re)
		}
	}
	return &Rules{List: doc.Rules}, nil
}

// Input is the part of Claude Code's PreToolUse payload the guard reads.
type Input struct {
	ToolName  string `json:"tool_name"`
	CWD       string `json:"cwd"`
	ToolInput struct {
		Command      string `json:"command"`
		FilePath     string `json:"file_path"`
		Content      string `json:"content"`
		NewString    string `json:"new_string"`
		SubagentType string `json:"subagent_type"`
	} `json:"tool_input"`
}

// Decision is the guard's answer. An empty Action lets the call through
// untouched.
type Decision struct {
	Action string // "", "deny" or "allow"
	Rule   string
	Reason string
}

// Evaluate runs the rules in order against one tool call. The first rule that
// matches answers. A check that panics denies the call: a guard that waves a
// command through because it broke on it is not a guard.
func (rs *Rules) Evaluate(in Input, env Env) (d Decision) {
	ctx := &evalCtx{in: in, env: env}
	var current *Rule
	defer func() {
		if p := recover(); p != nil {
			id := "?"
			if current != nil {
				id = current.ID
			}
			d = Decision{Action: "deny", Rule: id,
				Reason: fmt.Sprintf("atrium's guard failed on this call (rule %s: %v), so it is refused. Rephrase it, or have the user run it.", id, p)}
		}
	}()
	for _, r := range rs.List {
		if !ctx.toolMatches(r) {
			continue
		}
		current = r
		hit, name := checks[r.Check](ctx, r)
		if hit {
			return Decision{Action: r.Decision, Rule: r.ID, Reason: strings.ReplaceAll(r.Reason, "{name}", name)}
		}
	}
	return Decision{}
}

// evalCtx is one call being evaluated. The script and the git analysis are
// made on first use, so a call no rule needs parsed is never parsed.
type evalCtx struct {
	in     Input
	env    Env
	script *Script
	gits   []*gitCall
	gitsOK bool
	hub    *bool
}

func (c *evalCtx) toolMatches(r *Rule) bool {
	for _, t := range r.Tools {
		if t == "shell" && c.in.ToolInput.Command != "" {
			return true
		}
		if strings.EqualFold(t, c.in.ToolName) {
			return true
		}
	}
	return false
}

// Script is the call's command, parsed by the shell that will run it: the
// PowerShell tool's by the pwsh tokenizer, everything else by bash.
func (c *evalCtx) Script() *Script {
	if c.script == nil {
		shell := "bash"
		if strings.EqualFold(c.in.ToolName, "PowerShell") {
			shell = "pwsh"
		}
		dir := c.in.CWD
		if dir != "" {
			dir = filepath.Clean(dir)
		}
		c.script = Parse(c.in.ToolInput.Command, shell, dir, c.env)
	}
	return c.script
}
