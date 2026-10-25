package rules

import (
	"strings"
	"testing"
)

// A title fetched from an issue tracker is somebody else's text, and somebody
// else's text has newlines in it. A card title is one line and a path is a path.
func TestFetchedTextDoesNotBreakOutOfItsField(t *testing.T) {
	t.Parallel()
	r := &Recogniser{
		ID: "x", Title: "{title}", Cwd: "d:/w/{repo}", Prompt: "fix {title}",
	}
	got := r.Fill(map[string]string{
		"title": "crash on start\r\nrm -rf everything",
		"repo":  "ziti\nelsewhere",
	})
	if strings.ContainsAny(got.Title, "\r\n") {
		t.Fatalf("a two-line title reached the card: %q", got.Title)
	}
	if strings.ContainsAny(got.Cwd, "\r\n") {
		t.Fatalf("a newline reached a path: %q", got.Cwd)
	}
	// The prompt is the exception on purpose: an issue body is paragraphs, and
	// flattening it would be atrium mangling somebody else's words in transit.
	if !strings.Contains(got.Prompt, "\n") {
		t.Fatalf("the prompt was flattened, so an issue body would arrive as one line: %q", got.Prompt)
	}
}

// FillPrompt takes the captures only: a fetched fact's placeholder is blank, an unknown one stays.
func TestFillPromptTakesOnlyTheCaptures(t *testing.T) {
	t.Parallel()
	r := &Recogniser{ID: "x", Prompt: "Work on {url} in {repo}.\n\n{body}{other}"}
	got := r.FillPrompt(map[string]string{"url": "https://h/o/r/issues/1", "repo": "r"}, map[string]bool{"body": true})
	if got != "Work on https://h/o/r/issues/1 in r.\n\n{other}" {
		t.Fatalf("prompt: %q", got)
	}
}

func TestFillArgvKeepsEveryArgumentSeparate(t *testing.T) {
	t.Parallel()
	cmd, args := FillArgv("gh", []string{"issue", "view", "{num}", "--repo", "{org}/{repo}"},
		map[string]string{"num": "12", "org": "open ziti", "repo": "ziti"})
	if cmd != "gh" {
		t.Fatalf("cmd: %q", cmd)
	}
	if len(args) != 5 {
		t.Fatalf("arguments were joined or split: %q", args)
	}
	if args[4] != "open ziti/ziti" {
		t.Fatalf("a value with a space in it stopped being one argument: %q", args[4])
	}
}
