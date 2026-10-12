// Package land rewrites the commits a branch has over a base so they can be pushed to GitHub: one-line messages with
// the agent's name scrubbed out, and one identity as author and committer. It replaces a `git filter-branch` that
// started about twenty processes per commit with one pass in Go that writes the commit objects itself.
package land

import (
	"regexp"
	"strings"
)

// rules run in order. A branch prefix goes first, so what remains is a word in a sentence.
var rules = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`\b([A-Za-z0-9_-]+)/claude/`), "$1/"},
	{regexp.MustCompile(`claude/main`), "main"},
	{regexp.MustCompile(`claude/`), ""},
	{regexp.MustCompile(`CLAUDE\.md`), "AGENTS.md"},
	{regexp.MustCompile(`build\.claude`), "the build directory"},
	{regexp.MustCompile(`CLAUDEVM`), "WORKVM"},
	{regexp.MustCompile(`(?i)claudevm`), "workvm"},
	{regexp.MustCompile(`claudeAuthCheck`), "authCheck"},
	{regexp.MustCompile(`(?i)claude-code`), "agent-cli"},
	{regexp.MustCompile(`(?i)Claude Code`), "the agent CLI"},
	{regexp.MustCompile(`claude-landing`), "landing"},
	{regexp.MustCompile(`claudeconf`), "agentconf"},
	{regexp.MustCompile(`CLAUDE`), "AGENT"},
	{regexp.MustCompile(`Claude`), "Agent"},
	{regexp.MustCompile(`(?i)claude`), "agent"},
}

// dropLine is a trailer naming a co-author or the vendor. The whole line goes.
var dropLine = regexp.MustCompile(`(?i)^\s*co.authored.by:|anthropic`)

// Banned matches what no landed message may contain.
var Banned = regexp.MustCompile(`(?i)claude|anthropic|co.authored.by`)

// Scrub applies the message rules and ends the message with one newline.
func Scrub(msg string) string {
	var keep []string
	for _, l := range strings.Split(msg, "\n") {
		if dropLine.MatchString(l) {
			continue
		}
		for _, r := range rules {
			l = r.re.ReplaceAllString(l, r.with)
		}
		keep = append(keep, l)
	}
	return strings.TrimRight(strings.Join(keep, "\n"), "\n \t") + "\n"
}
