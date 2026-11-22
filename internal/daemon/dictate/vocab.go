package dictate

import (
	"regexp"
	"strings"
)

// Words is what every prompt carries: the atrium words a person says that a speech model spells as prose.
var Words = []string{
	"atrium", "atrium_say", "atrium_done", "atrium_blocked", "atrium_report", "atrium_merge", "claude/main",
	"orchestrator", "hub", "room", "card", "worker", "ziti", "zrok", "sha", "rebase", "worktree", "walk", "mock",
	"repo", "PR", "CI", "Claude Code", "codex",
}

// promptMax keeps the prompt inside whisper's 224 token window with room to spare.
const promptMax = 700

// Prompt is the initial prompt: the atrium words, then the live handles and repos, deduplicated and capped.
func Prompt(extra []string) string {
	seen := map[string]bool{}
	var b strings.Builder
	for _, w := range append(append([]string{}, Words...), extra...) {
		w = strings.TrimSpace(w)
		k := strings.ToLower(w)
		if w == "" || seen[k] {
			continue
		}
		if b.Len()+len(w)+2 > promptMax {
			break
		}
		seen[k] = true
		if b.Len() > 0 {
			b.WriteString(", ")
		}
		b.WriteString(w)
	}
	return b.String()
}

var (
	reNoise  = regexp.MustCompile(`\[[A-Z_ ]+\]|\((?i:silence|music|blank_audio)\)`)
	reSpace  = regexp.MustCompile(`\s+`)
	reTool   = regexp.MustCompile(`(?i)\batrium[ -](say|done|blocked|report|merge|status|need|get|put|files|peers|resources|remind)\b`)
	reSlash  = regexp.MustCompile(`(?i)\s*\bslash\b\s*`)
	reSplits = regexp.MustCompile(`[-_]+`)
)

// Clean is the post-pass: one line, "atrium say" to `atrium_say`, "slash" to `/`, and a spoken handle back to its
// spelling. It never adds a carriage return.
func Clean(text string, handles []string) string {
	text = reNoise.ReplaceAllString(text, " ")
	text = reSpace.ReplaceAllString(text, " ")
	text = reTool.ReplaceAllStringFunc(text, func(m string) string {
		return "atrium_" + strings.ToLower(reTool.FindStringSubmatch(m)[1])
	})
	text = reSlash.ReplaceAllString(text, "/")
	for _, h := range handles {
		parts := reSplits.Split(strings.TrimSpace(h), -1)
		if len(parts) < 2 {
			continue
		}
		for i, p := range parts {
			parts[i] = regexp.QuoteMeta(p)
		}
		re, err := regexp.Compile(`(?i)\b` + strings.Join(parts, `[\s_-]+`) + `\b`)
		if err != nil {
			continue
		}
		text = re.ReplaceAllLiteralString(text, h)
	}
	return strings.TrimSpace(text)
}
