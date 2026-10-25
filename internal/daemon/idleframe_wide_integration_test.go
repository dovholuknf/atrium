//go:build integration

package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon/screen"
)

// The heuristics mask a stray U+FFFD, so this looks at the text itself: a wide
// character reaches classifyScreen once, with no replacement after it.
func TestClassifyFrameTextHasEachWideCharacterOnce(t *testing.T) {
	t.Parallel()
	frame := "\x1b[H\x1b[2J✻ Crunched for 1m 0s\r\n\r\n" + frameRule + "\r\n❯ 日本語のテキスト\r\n" + frameRule +
		"\r\n  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents\r\n"
	sc := screen.NewScreenSized(80, 40)
	sc.Apply([]byte(frame))
	text := screen.FrameText(sc)
	if strings.Contains(text, "�") {
		t.Fatalf("replacement character in %q", text)
	}
	if !strings.Contains(text, "❯ 日本語のテキスト ") {
		t.Fatalf("prompt not intact in %q", text)
	}
	if idle, why := screen.ClassifyScreen(text); !idle || why != screen.FrameIdle {
		t.Fatalf("got %v/%s, want idle", idle, why)
	}
	if idle, why := screen.ClassifyFrame([]byte(frame), 80, 40); !idle || why != screen.FrameIdle {
		t.Fatalf("classifyFrame got %v/%s", idle, why)
	}
}
