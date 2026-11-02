//go:build integration

package pulls

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

var changesBase = time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)

func transcriptReply(t *testing.T, path, id string, at time.Time, sidechain bool, blocks ...map[string]any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{
		"type": "assistant", "timestamp": at.Format(time.RFC3339Nano), "isSidechain": sidechain,
		"message": map[string]any{"id": id, "content": blocks},
	})
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	fmt.Fprintln(fh, string(b))
}

func toolUse(name, path string) map[string]any {
	return map[string]any{"type": "tool_use", "name": name, "input": map[string]any{"file_path": path}}
}
func TestTurnsOfReadsOnlyWhatTheTranscriptGained(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	edit := func(id string, at time.Time, p string) {
		transcriptReply(t, path, id, at, false, toolUse("Edit", p))
	}
	for i := 0; i < 200; i++ {
		edit(fmt.Sprintf("m%d", i), changesBase.Add(time.Duration(i)*time.Second), "/work/old.txt")
	}
	x, err := TurnsOf(path)
	if err != nil || len(x.edits) != 200 {
		t.Fatalf("first read: %v %d", err, len(x.edits))
	}
	info, _ := os.Stat(path)
	before := turnScanned.Load()
	edit("new", changesBase.Add(time.Hour), "/work/new.txt")
	x, err = TurnsOf(path)
	if err != nil || len(x.edits) != 201 || x.edits[200].path != "/work/new.txt" {
		t.Fatalf("after growth: %v %d", err, len(x.edits))
	}
	if read := turnScanned.Load() - before; read <= 0 || read > info.Size()/10 {
		t.Fatalf("a grown file was read for %d bytes of %d: it should start from where it stopped", read, info.Size())
	}

	// A file that got shorter is read again from the start.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	edit("only", changesBase, "/work/only.txt")
	if x, err = TurnsOf(path); err != nil || len(x.edits) != 1 || x.edits[0].path != "/work/only.txt" {
		t.Fatalf("after truncation: %v %+v", err, x.edits)
	}

	// A replacement that is longer than what was read is told by its bytes.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		edit(fmt.Sprintf("r%d", i), changesBase.Add(time.Duration(i)*time.Second), fmt.Sprintf("/work/replaced%d.txt", i))
	}
	if x, err = TurnsOf(path); err != nil || len(x.edits) != 50 || x.edits[0].path != "/work/replaced0.txt" {
		t.Fatalf("after replacement: %v %d", err, len(x.edits))
	}

	// A line still being written is read once it has its newline.
	fh, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	b, _ := json.Marshal(map[string]any{"type": "assistant", "timestamp": changesBase.Add(time.Hour).Format(time.RFC3339Nano),
		"message": map[string]any{"id": "half", "content": []map[string]any{toolUse("Edit", "/work/half.txt")}}})
	fh.Write(b[:len(b)/2])
	if x, _ = TurnsOf(path); len(x.edits) != 50 {
		t.Fatalf("a half line was read: %d", len(x.edits))
	}
	fh.Write(b[len(b)/2:])
	fh.WriteString("\n")
	fh.Close()
	if x, _ = TurnsOf(path); len(x.edits) != 51 {
		t.Fatalf("the finished line was not read: %d", len(x.edits))
	}
}

func TestUntrackedTrimsEachCappedListingToWholeNames(t *testing.T) {
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-b", "claude/main").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var paths []string
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("untracked-name-%03d.txt", i)
		if err := os.WriteFile(filepath.Join(repo, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, name)
	}
	g := gitAt{ctx: context.Background(), run: gitsync.Default, dir: repo, listMax: 100}
	// Chunks of one name each: every chunk is under the cap, then chunks of all names at once are not.
	for _, ps := range [][]string{paths[:3], paths} {
		for _, file := range untracked(g, repo, ps) {
			if _, err := os.Stat(filepath.Join(repo, file.Path)); err != nil {
				t.Fatalf("a name cut mid-way was listed: %q", file.Path)
			}
		}
	}
	if got := untracked(g, repo, nil); len(got) == 0 || len(got) >= 40 {
		t.Fatalf("a listing past the cap should be cut to its whole names, got %d", len(got))
	}
	for _, file := range untracked(g, repo, nil) {
		if _, err := os.Stat(filepath.Join(repo, file.Path)); err != nil {
			t.Fatalf("a name cut mid-way was listed: %q", file.Path)
		}
	}
}
