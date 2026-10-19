//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func fillHome(t *testing.T, dir string, files int, size int) {
	t.Helper()
	for i := 0; i < files; i++ {
		sub := filepath.Join(dir, "d"+string(rune('a'+i%3)))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "f"+string(rune('a'+i))), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheSizeWalkStopsAtItsBudgetAndResumes(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	row, dd, err := d.makeHome(store.CardHomeScratch, "t", "w")
	if err != nil {
		t.Fatal(err)
	}
	fillHome(t, dd.Dir, 6, 100)

	if d.walkHome(row.Path, time.Now().Add(-time.Second)) {
		t.Fatal("a walk past its deadline said it was done")
	}
	if got, _, _ := d.st.CardHomeByPath(row.Path); got.MeasuredAt != "" {
		t.Fatalf("an unfinished walk wrote a size: %+v", got)
	}
	if !d.walkHome(row.Path, time.Now().Add(time.Minute)) {
		t.Fatal("the walk did not finish with time")
	}
	got, _, _ := d.st.CardHomeByPath(row.Path)
	if got.Files < 6 || got.Bytes < 600 || got.MeasuredAt == "" {
		t.Fatalf("size not written: %+v", got)
	}
}

func TestTheSizeWalkDoesNotFollowALink(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	row, dd, err := d.makeHome(store.CardHomeScratch, "t", "w")
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	fillHome(t, outside, 3, 5000)
	if err := os.Symlink(outside, filepath.Join(dd.Work, "link")); err != nil {
		t.Skipf("no links here: %v", err)
	}
	if !d.walkHome(row.Path, time.Now().Add(time.Minute)) {
		t.Fatal("the walk did not finish")
	}
	if got, _, _ := d.st.CardHomeByPath(row.Path); got.Bytes >= 5000 {
		t.Fatalf("the walk counted what a link points at: %d bytes", got.Bytes)
	}
}

func TestTheDiskViewListsHomesBySizeAndWhatIsNotAtriums(t *testing.T) {
	t.Parallel()
	d, root := homeRoom(t)
	small, ds, err := d.makeHome(store.CardHomeScratch, "small", "w")
	if err != nil {
		t.Fatal(err)
	}
	big, db, err := d.makeHome(store.CardHomeScratch, "big", "w")
	if err != nil {
		t.Fatal(err)
	}
	fillHome(t, ds.Dir, 1, 10)
	fillHome(t, db.Dir, 3, 4000)
	d.walkHome(small.Path, time.Now().Add(time.Minute))
	d.walkHome(big.Path, time.Now().Add(time.Minute))
	if err := os.MkdirAll(filepath.Join(root, "stray"), 0o755); err != nil {
		t.Fatal(err)
	}
	outCard, _, err := d.st.Register(store.Observed{WireName: "elsewhere", Worktree: filepath.ToSlash(t.TempDir()), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	any, err := d.DiskView()
	if err != nil {
		t.Fatal(err)
	}
	v := any.(DiskView)
	if len(v.Homes) != 2 || v.Homes[0].Path != big.Path || v.Homes[0].Bytes < v.Homes[1].Bytes {
		t.Fatalf("homes not by size: %+v", v.Homes)
	}
	if len(v.NotAtriums) != 1 || !strings.HasSuffix(v.NotAtriums[0], "/stray") {
		t.Fatalf("not atrium's: %v", v.NotAtriums)
	}
	if len(v.Outside) != 1 || v.Outside[0].Card != outCard.ID {
		t.Fatalf("outside: %+v", v.Outside)
	}
	if v.Bytes != v.Homes[0].Bytes+v.Homes[1].Bytes {
		t.Fatalf("total %d", v.Bytes)
	}
}

func TestRemoveHomeRefusesALiveCardAndRemovesAKeptOrphan(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	row, dd, err := d.makeHome(store.CardHomeScratch, "t", "w")
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := d.st.Register(store.Observed{WireName: "w", Worktree: filepath.ToSlash(dd.Work), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.CardHomeCard(row.Path, task.ID); err != nil {
		t.Fatal(err)
	}
	row, _, _ = d.st.CardHomeByPath(row.Path)
	if _, err := d.RemoveHome(row.ID); err == nil {
		t.Fatal("a live card's home was removed")
	}
	if _, err := os.Stat(dd.Dir); err != nil {
		t.Fatal("the refused removal touched the home")
	}
	if _, err := d.RemoveHome("no-such-id"); err == nil {
		t.Fatal("an unknown id was accepted")
	}

	if err := d.st.CardHomeSet(row.Path, store.CardHomeKept, "dirty", ""); err != nil {
		t.Fatal(err)
	}
	res, err := d.RemoveHome(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(HomeRemoved); !r.Removed {
		t.Fatalf("a kept home stayed: %+v", r)
	}
	if _, err := os.Stat(dd.Dir); !os.IsNotExist(err) {
		t.Fatal("the home is still there")
	}
}

func TestCullKeepsTheBranchWhenTheHubDoesNotHaveIt(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	task, row, dd := worktreeHome(t, d, repo)
	if err := os.WriteFile(filepath.Join(dd.Work, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	cullGit(t, dd.Work, "add", "a.txt")
	cullGit(t, dd.Work, "commit", "-q", "-m", "a")
	tip := strings.TrimSpace(cullGit(t, dd.Work, "rev-parse", "HEAD"))
	cullGit(t, repo, "checkout", "-q", "claude/main")
	cullGit(t, repo, "cherry-pick", tip)
	cullGit(t, repo, "checkout", "-q", "main")

	res, err := d.cullHome(task, DefaultCullInto, row)
	if err != nil {
		t.Fatal(err)
	}
	if !res.WorktreeRemoved || res.Kept != "" {
		t.Fatalf("a landed home was not removed: %+v", res)
	}
	if res.BranchDeleted {
		t.Fatalf("BranchDeleted claimed a branch the hub never had: %+v", res)
	}
	if out := cullGit(t, repo, "branch", "--list", "claude/fix-the-thing"); strings.TrimSpace(out) == "" {
		t.Fatal("the branch was deleted")
	}
}
