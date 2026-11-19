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

// A person launching from a shell is not told about backlog items.
func TestAtriumLaunchWithNoItemSaysNothingAboutItems(t *testing.T) {
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	_ = launchAgent(launchOpts{boardURL: "http://127.0.0.1:1", cwd: t.TempDir()})
	os.Stdout = old
	w.Close()
	out, _ := io.ReadAll(r)
	if strings.Contains(string(out), "backlog item") {
		t.Fatalf("out = %q", out)
	}
}

func TestLaunchPrintsTheHubBoardNotTheRoomsOwn(t *testing.T) {
	t.Setenv("ATRIUM_BOARD_URL", "")
	t.Setenv("WORKTREE_ROOT", t.TempDir())
	if got := hubBoardAddress(""); got != "http://127.0.0.1:7778" {
		t.Fatalf("got %q", got)
	}
	if err := writeHubAddrs(hubDir(), hubAddrs{Board: "localhost:9778", Link: "127.0.0.1:9779"}); err != nil {
		t.Fatal(err)
	}
	if got := hubBoardAddress(""); got != "http://127.0.0.1:9778" {
		t.Fatalf("got %q", got)
	}
	if got := hubBoardAddress("10.0.0.2:9000"); got != "http://10.0.0.2:9000" {
		t.Fatalf("got %q", got)
	}
}
