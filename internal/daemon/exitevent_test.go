package daemon

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestExitEventIsAWarningOnlyForAnUnaskedFailure(t *testing.T) {
	var b bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&b)
	defer log.SetOutput(prev)
	exitEvent(7, 1, "", "")
	exitEvent(8, 0, "asked", "you")
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "pid=7") || !strings.HasSuffix(lines[0], "level=warn") ||
		!strings.Contains(lines[1], `reason="asked, exit code 0" by="you"`) || !strings.HasSuffix(lines[1], "level=info") {
		t.Fatalf("lines = %q", lines)
	}
}
