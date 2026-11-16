package cli

import (
	"io"
	"os"
	"strings"
	"testing"
)

// A NEW LAUNCH WITH NO --item GOES ON TO THE POST. The board address is one nothing listens on, so the post is what fails.
func TestAtriumLaunchWithNoItemIsPosted(t *testing.T) {
	err := launchAgent(launchOpts{boardURL: "http://127.0.0.1:1", cwd: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "no daemon answered") {
		t.Fatalf("err = %v", err)
	}
}

// A new launch with no item says so.
func TestAtriumLaunchWithNoItemSaysNoneWasAttached(t *testing.T) {
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	_ = launchAgent(launchOpts{boardURL: "http://127.0.0.1:1", cwd: t.TempDir()})
	os.Stdout = old
	w.Close()
	out, _ := io.ReadAll(r)
	if !strings.Contains(string(out), "no backlog item was attached") {
		t.Fatalf("out = %q", out)
	}
}
