// Package mcprule is what a standing rule may say about an MCP tool, in one place
// because the store and the settings importer both have to agree on it and neither
// may import the other.
//
// An MCP rule matches the tool NAME and nothing else. See
// docs/runtime/mcp-rules-design.md for why not the arguments.
package mcprule

import (
	"regexp"
	"strings"
)

// Prefix begins every MCP tool name Claude Code reports.
const Prefix = "mcp__"

// AnyInput is the pattern half of an MCP rule. The rule is about the tool, so the
// input is not consulted.
const AnyInput = "*"

// The reasons a name is refused. They are shown to the operator on the import
// dialog and by the rules API, so they are written to be read.
const (
	ReasonArguments = "MCP rules match the tool name, not its arguments"
	ReasonServer    = "the server name must be spelled out"
	ReasonUsable    = "no usable pattern"
)

// Is reports whether a tool name is an MCP tool.
func Is(tool string) bool { return strings.HasPrefix(tool, Prefix) }

// Normalize turns what an operator wrote into the name a rule stores, or says why
// it cannot.
//
// The accepted forms follow Claude Code's MCP permission syntax, read on
// 2026-09-29 at https://code.claude.com/docs/en/permissions (section "MCP" and
// "Tool name wildcards"). That page moves, and nothing here checks it:
//
//	mcp__server__tool     that tool
//	mcp__server__pre_*    a glob, allowed only AFTER a literal mcp__server__
//	mcp__server__*        every tool of the server
//	mcp__server           the same, and stored as mcp__server__*
//
// The server segment has to be glob-free, so a rule names a server somebody
// configured. `mcp__*` is refused here: Claude Code accepts it as a deny, and the
// importer handles that one itself because it is Broad.
//
// The bare form is stored with its trailing `__*` and never as it was typed. A
// bare prefix would also cover a server called `mcp__server-staging`.
func Normalize(name string) (string, string) {
	name = strings.TrimSpace(name)
	if !Is(name) {
		return "", ReasonUsable
	}
	if strings.Contains(name, "(") || strings.Contains(name, ")") {
		return "", ReasonArguments
	}
	if strings.ContainsAny(name, " \t\r\n?") {
		return "", ReasonUsable
	}
	rest := name[len(Prefix):]
	server, tool, hasTool := strings.Cut(rest, "__")
	if server == "" {
		return "", ReasonUsable
	}
	if strings.Contains(server, "*") {
		return "", ReasonServer
	}
	if !hasTool {
		return Prefix + server + "__*", ""
	}
	if tool == "" {
		return "", ReasonUsable
	}
	return Prefix + server + "__" + tool, ""
}

// Covers reports whether a glob names another rule's tool. Both are tool names
// and only `*` is a wildcard. The other side is read as literal text, so a deny of
// `mcp__s__*` covers an allow of `mcp__s__get_*` and not the reverse: a deny that
// is narrower than an allow leaves the allow standing, which is the store's own
// answer by specificity.
func Covers(glob, name string) bool {
	if !strings.Contains(glob, "*") {
		return glob == name
	}
	var b strings.Builder
	b.WriteString(`\A`)
	for _, r := range glob {
		if r == '*' {
			b.WriteString(`.*`)
			continue
		}
		b.WriteString(regexp.QuoteMeta(string(r)))
	}
	b.WriteString(`\z`)
	re, err := regexp.Compile(b.String())
	return err == nil && re.MatchString(name)
}
