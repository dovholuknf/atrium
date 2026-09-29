package daemon

import (
	"strings"
	"testing"
)

// The heuristics mask a stray U+FFFD, so this looks at the text itself: a wide
// character reaches classifyScreen once, with no replacement after it.
func TestClassifyFrameTextHasEachWideCharacterOnce(t *testing.T) {
	frame := "\x1b[H\x1b[2J✻ Crunched for 1m 0s\r\n\r\n" + frameRule + "\r\n❯ 日本語のテキスト\r\n" + frameRule +
		"\r\n  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents\r\n"
	sc := newScreenSized(80, 40)
	sc.apply([]byte(frame))
	text := frameText(sc)
	if strings.Contains(text, "�") {
		t.Fatalf("replacement character in %q", text)
	}
	if !strings.Contains(text, "❯ 日本語のテキスト ") {
		t.Fatalf("prompt not intact in %q", text)
	}
	if idle, why := classifyScreen(text); !idle || why != frameIdle {
		t.Fatalf("got %v/%s, want idle", idle, why)
	}
	if idle, why := classifyFrame([]byte(frame), 80, 40); !idle || why != frameIdle {
		t.Fatalf("classifyFrame got %v/%s", idle, why)
	}
}
