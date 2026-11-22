package dictate

import (
	"strings"
	"testing"
)

func TestCleanSpellsAtriumWordsAndHandles(t *testing.T) {
	in := " Ask the hub worker to rebase onto claude slash main,\n then Atrium say the sha to voice push to talk.\n [BLANK_AUDIO]"
	got := Clean(in, []string{"voice-push-to-talk", "sg4"})
	want := "Ask the hub worker to rebase onto claude/main, then atrium_say the sha to voice-push-to-talk."
	if got != want {
		t.Fatalf("Clean:\n got %q\nwant %q", got, want)
	}
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("Clean left a line break: %q", got)
	}
}

func TestCleanSlashCommandAtTheStart(t *testing.T) {
	if got := Clean("slash compact", nil); got != "/compact" {
		t.Fatalf("got %q", got)
	}
}

func TestPromptDedupsAndCaps(t *testing.T) {
	p := Prompt([]string{"atrium", "voice-push-to-talk", strings.Repeat("x", 800)})
	if strings.Count(p, "atrium,") != 1 || !strings.Contains(p, "voice-push-to-talk") {
		t.Fatalf("prompt: %q", p)
	}
	if len(p) > promptMax {
		t.Fatalf("prompt is %d bytes, over %d", len(p), promptMax)
	}
}
