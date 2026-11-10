package docs

import (
	"context"
	"embed"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
)

// A prompt is the hub document `prompt-<name>`, versioned like any other. prompts/ ship with atrium and answer only
// while the hub has no document of that name, so a hub copy replaces one without a deploy.
//
//go:embed prompts/*.md
var builtinPrompts embed.FS

// PromptSlugPrefix marks a hub document as a prompt.
const PromptSlugPrefix = "prompt-"

const promptMaxBytes = 256 << 10

const promptToolDesc = "The prompt library: named, reusable task recipes. Call `list` when a task looks like a " +
	"known recipe (an explainer, a review, a write-up) or a brief says \"follow the prompt <name>\", then `get` the " +
	"one you need and follow it.\n\n" +
	"`list` answers each prompt's name and one-line summary. `get` with `name` answers the full text of the newest " +
	"version, and `version` picks an older one. Fill-in fields such as FEATURE or OUT sit at the top of the text and " +
	"are yours to fill.\n\n" +
	"A prompt is the hub document `prompt-<name>`: publish one with atrium_publish and the title \"prompt <name>\", " +
	"and a new version with its slug."

type PromptInput struct {
	Action  string `json:"action" jsonschema:"list or get"`
	Name    string `json:"name,omitempty" jsonschema:"the prompt to get, as list names it"`
	Version int    `json:"version,omitempty" jsonschema:"an older version to get. leave out for the newest"`
}

type PromptSummary struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type PromptOutput struct {
	Prompts []PromptSummary `json:"prompts,omitempty"`
	Name    string          `json:"name,omitempty"`
	Version int             `json:"version,omitempty"`
	// Builtin is a prompt that ships with atrium and has no hub document yet. It has no versions.
	Builtin bool   `json:"builtin,omitempty"`
	Text    string `json:"text,omitempty"`
}

func (c *MCP) RegisterPrompts(s *mcp.Server, class ctl.Class) {
	ctl.AddTool(s, class, &mcp.Tool{Name: "atrium_prompt", Description: promptToolDesc}, c.PromptHandler)
}

func (c *MCP) PromptHandler(_ context.Context, _ *mcp.CallToolRequest, in PromptInput) (
	*mcp.CallToolResult, PromptOutput, error) {

	var st *hubstore.Store
	if c.Docs != nil {
		st = c.Docs()
	}
	switch strings.TrimSpace(in.Action) {
	case "list":
		out, err := promptList(st)
		return nil, out, err
	case "get":
		out, err := promptGet(st, strings.TrimSpace(in.Name), in.Version)
		return nil, out, err
	}
	return nil, PromptOutput{}, &Refusal{Status: http.StatusBadRequest, Msg: "action is list or get"}
}

func promptList(st *hubstore.Store) (PromptOutput, error) {
	byName := map[string]string{}
	ents, _ := builtinPrompts.ReadDir("prompts")
	for _, e := range ents {
		name := strings.TrimSuffix(e.Name(), ".md")
		b, err := builtinPrompts.ReadFile("prompts/" + e.Name())
		if err != nil {
			return PromptOutput{}, err
		}
		byName[name] = promptSummary(string(b))
	}
	if st != nil {
		docs, err := st.DocList("", false, "")
		if err != nil {
			return PromptOutput{}, err
		}
		for _, d := range docs {
			name, ok := strings.CutPrefix(d.Slug, PromptSlugPrefix)
			if !ok || name == "" {
				continue
			}
			text, _, err := readDoc(st, d.Slug, 0)
			if err != nil {
				byName[name] = "(unreadable: " + err.Error() + ")"
				continue
			}
			byName[name] = promptSummary(text)
		}
	}
	out := PromptOutput{Prompts: []PromptSummary{}}
	for n, s := range byName {
		out.Prompts = append(out.Prompts, PromptSummary{Name: n, Summary: s})
	}
	sort.Slice(out.Prompts, func(i, j int) bool { return out.Prompts[i].Name < out.Prompts[j].Name })
	return out, nil
}

func promptGet(st *hubstore.Store, name string, version int) (PromptOutput, error) {
	if name == "" || !hubstore.ValidDocSlug(PromptSlugPrefix+name) {
		return PromptOutput{}, &Refusal{Status: http.StatusBadRequest,
			Msg: "name is a prompt's name as list gives it: lower case letters, digits and hyphens"}
	}
	if st != nil {
		text, n, err := readDoc(st, PromptSlugPrefix+name, version)
		if err == nil {
			return PromptOutput{Name: name, Version: n, Text: text}, nil
		}
		if de, ok := hubstore.AsDocError(err); !ok || de.Kind != hubstore.DocNone || version > 0 {
			return PromptOutput{}, publishError(err)
		}
	}
	if version > 0 {
		return PromptOutput{}, &Refusal{Status: http.StatusNotFound,
			Msg: fmt.Sprintf("%s ships with atrium and has no versions on this hub", name)}
	}
	b, err := builtinPrompts.ReadFile("prompts/" + name + ".md")
	if err != nil {
		return PromptOutput{}, &Refusal{Status: http.StatusNotFound, Msg: "there is no prompt called " + name + ". call list"}
	}
	return PromptOutput{Name: name, Builtin: true, Text: string(b)}, nil
}

func readDoc(st *hubstore.Store, slug string, n int) (string, int, error) {
	f, v, err := st.DocOpen(slug, n)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, promptMaxBytes+1))
	if err != nil {
		return "", 0, err
	}
	if len(b) > promptMaxBytes {
		return "", 0, &Refusal{Status: http.StatusRequestEntityTooLarge,
			Msg: fmt.Sprintf("%s is over the %d byte cap for a prompt", slug, promptMaxBytes)}
	}
	return string(b), v.N, nil
}

// promptSummary is the first line with text, less a heading's hashes.
func promptSummary(text string) string {
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#"))
		if l != "" {
			if r := []rune(l); len(r) > 200 {
				l = string(r[:200])
			}
			return l
		}
	}
	return ""
}
