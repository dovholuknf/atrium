package cli

import (
	"strings"
	"testing"
)

// A NEW LAUNCH WITH NO --item IS REFUSED before anything is posted, and the refusal names the flag. The board address
// is one nothing listens on, so a post would fail differently.
func TestAtriumLaunchWithNoItemIsRefusedNamingTheFlag(t *testing.T) {
	err := launchAgent(launchOpts{boardURL: "http://127.0.0.1:1", cwd: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "--item") {
		t.Fatalf("err = %v", err)
	}
}
