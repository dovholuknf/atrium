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

// The prep notice: one line at a set percent of the card's limit, once per conversation, before the cycle.

const prepWords = "write your handoff now"

func prepNotices(f *fakePTY) int { return strings.Count(f.written(), prepWords) }

func TestPrepNoticeThresholdsScaleWithTheLimit(t *testing.T) {
	d := testDaemon(t)
	for _, c := range []struct{ limitK, atK int64 }{{200, 160}, {400, 320}, {1000, 800}} {
		if got := d.prepAtTokens(c.limitK * 1000); got != c.atK*1000 {
			t.Errorf("limit %dk: prep at %d, want %dk", c.limitK, got, c.atK)
		}
	}
	setK(t, d, store.SettingContextPrepPct, "50")
	if got := d.prepAtTokens(400_000); got != 200_000 {
		t.Errorf("50%% of 400k = %d", got)
	}
	setK(t, d, store.SettingContextPrepPct, "0")
	if got := d.prepAtTokens(400_000); got != 0 {
		t.Errorf("prep off gave %d", got)
	}
	if _, err := store.CheckContextPrepPct("100"); err == nil {
		t.Error("100 percent was accepted")
	}
}

func testPrepOnce(t *testing.T, limit string, belowK, atK int64, wantLimitK string) {
	t.Setenv("HOME", t.TempDir()) // no Claude Code settings: no runner ceiling
	t.Setenv("USERPROFILE", t.TempDir())
	d := testDaemon(t)
	setLimits(t, d, limit)
	task, f, reply := cycleCard(t, d, belowK*1000)
	statuslines(d, task.ID, 2)
	if n := prepNotices(f); n != 0 {
		t.Fatalf("%dk is under the prep line and %d notices went", belowK, n)
	}
	reply(atK * 1000)
	statuslines(d, task.ID, 1)
	until(t, "the prep notice", func() bool { return prepNotices(f) == 1 })
	if !strings.Contains(f.written(), "cycle your context at "+wantLimitK+"k") {
		t.Fatalf("the notice does not name the limit %sk: %q", wantLimitK, f.written())
	}
	// Climbing on, and a burst of updates, say nothing more.
	reply((atK + 5) * 1000)
	statuslines(d, task.ID, 5)
	time.Sleep(50 * time.Millisecond)
	if n := prepNotices(f); n != 1 {
		t.Fatalf("%d prep notices, want exactly one", n)
	}
	if prompts(f) != 0 {
		t.Fatal("the cycle started under the limit")
	}
}

func TestPrepNoticeAtEightyPercentOfA200kLimitOnce(t *testing.T) {
	testPrepOnce(t, `{"claude":200}`, 150, 160, "200")
}

func TestPrepNoticeAtEightyPercentOfA400kLimitOnce(t *testing.T) {
	fastNewContext(t)
	testPrepOnce(t, `{"claude":400}`, 310, 320, "400")
}

func TestPrepNoticeOffSaysNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	d := testDaemon(t)
	setLimits(t, d, `{"claude":200}`)
	setK(t, d, store.SettingContextPrepPct, "0")
	task, f, _ := cycleCard(t, d, 190_000)
	statuslines(d, task.ID, 2)
	time.Sleep(50 * time.Millisecond)
	if prepNotices(f) != 0 {
		t.Fatal("a prep notice went with the setting off")
	}
}

// A new conversation, as after the cycle's /clear, is told again, and its own limit is named.
func TestPrepNoticeIsOncePerConversation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	d := testDaemon(t)
	setLimits(t, d, `{"claude":200}`)
	task, f, _ := cycleCard(t, d, 170_000)
	statuslines(d, task.ID, 1)
	until(t, "the first notice", func() bool { return prepNotices(f) == 1 })
	if err := d.st.SetResumeID(task.ID, "second-conversation"); err != nil {
		t.Skipf("no way to start a new conversation in this test: %v", err)
	}
	task, _ = d.st.Get(task.ID)
	d.cycleCheck(task, 170_000)
	until(t, "the second conversation's notice", func() bool { return prepNotices(f) == 2 })
}

// Claude Code's autoCompactWindow of 400000 compacts at 367k, so a 400k limit is cut to 333k and the
// cycle comes before the runner's compaction, with the prep notice at 80% of THAT.
func TestCycleComesBeforeTheRunnersCompactAndPrepScalesToIt(t *testing.T) {
	fastNewContext(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"),
		[]byte(`{"autoCompactWindow": 400000}`), 0o644); err != nil {
		t.Fatal(err)
	}
	d := testDaemon(t)
	setLimits(t, d, `{"claude":400}`)
	task, f, reply := cycleCard(t, d, 250_000)
	if at := d.runnerCompactAtK(task); at != 367 {
		t.Fatalf("runner compacts at %dk, want 367k", at)
	}
	limit := d.cycleLimit(task)
	if limit != 333_000 || limit >= 367_000 {
		t.Fatalf("cycle limit %d, want 333000 and under the runner's 367k", limit)
	}
	statuslines(d, task.ID, 1)
	time.Sleep(50 * time.Millisecond)
	if prepNotices(f) != 0 {
		t.Fatal("notice at 250k, under 80% of 333k")
	}
	reply(270_000)
	statuslines(d, task.ID, 1)
	until(t, "the prep notice at 80% of 333k", func() bool { return prepNotices(f) == 1 })
	if !strings.Contains(f.written(), "at 333k") {
		t.Fatalf("notice names the wrong limit: %q", f.written())
	}
	reply(334_000)
	statuslines(d, task.ID, 1)
	until(t, "the cycle at the limit", func() bool { return prompts(f) == 1 })
}
