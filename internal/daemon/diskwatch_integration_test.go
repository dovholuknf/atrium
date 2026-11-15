//go:build integration

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/daemon/home"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

const hugeMB = 1 << 30

func setLevels(t *testing.T, d *Daemon, warnMB, criticalMB int) {
	t.Helper()
	for k, v := range map[string]int{store.SettingDiskWarnFree: warnMB, store.SettingDiskCriticalFree: criticalMB} {
		if err := d.st.SetSetting(k, strconv.Itoa(v)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDiskLevelIsOkWarningOrCritical(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		free uint64
		want string
	}{{10, DiskOK}, {5, DiskOK}, {4, DiskWarning}, {2, DiskWarning}, {1, DiskCritical}, {0, DiskCritical}} {
		if got := DiskLevel(c.free, 5, 2); got != c.want {
			t.Errorf("free %d: %s, wanted %s", c.free, got, c.want)
		}
	}
}

func TestADriveWithEnoughFreeSpaceIsOkAndLaunchesGo(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	setLevels(t, d, 1, 1)
	d.diskTick(context.Background(), time.Now())
	if st := d.DiskState().(DiskStatus); st.Level != DiskOK || st.Say != "" || len(st.Drives) == 0 {
		t.Fatalf("state %+v, wanted ok with drives and nothing to say", st)
	}
	if err := d.launchDiskFull(shared.LaunchRequest{}); err != nil {
		t.Fatalf("launch refused: %v", err)
	}
}

func TestADriveUnderTheWarningLevelIsNamedWithItsFreeSpaceAndLaunchesStillGo(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	setLevels(t, d, hugeMB, 1)
	d.diskTick(context.Background(), time.Now())
	st := d.DiskState().(DiskStatus)
	if st.Level != DiskWarning || !strings.Contains(st.Say, "free, under the warning level") || st.Drives[0].Drive == "" {
		t.Fatalf("state %+v, wanted a warning naming the drive and its free space", st)
	}
	if err := d.launchDiskFull(shared.LaunchRequest{}); err != nil {
		t.Fatalf("a warning refused a launch: %v", err)
	}
}

func TestADriveUnderTheCriticalLevelRefusesNewLaunchesAndSaysWhy(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	setLevels(t, d, hugeMB, hugeMB)
	d.diskTick(context.Background(), time.Now())
	if st := d.DiskState().(DiskStatus); st.Level != DiskCritical {
		t.Fatalf("level %s, wanted critical", st.Level)
	}
	err := d.launchDiskFull(shared.LaunchRequest{})
	if err == nil || !strings.Contains(err.Error(), "critical level") || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("launch answered %v, wanted a refusal that says why", err)
	}
	if _, err := d.Launch(shared.LaunchRequest{}); err == nil || !strings.Contains(err.Error(), "critical level") {
		t.Fatalf("Launch answered %v, wanted the same refusal", err)
	}
	if err := d.launchDiskFull(shared.LaunchRequest{TaskID: "t1"}); err != nil {
		t.Fatalf("a resume was refused: %v", err)
	}
	setLevels(t, d, 1, 1)
	d.diskTick(context.Background(), time.Now())
	if err := d.launchDiskFull(shared.LaunchRequest{}); err != nil {
		t.Fatalf("launch still refused after the space came back: %v", err)
	}
}

func TestACriticalLevelAboveTheWarningLevelIsHeldToIt(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	setLevels(t, d, 100, 500)
	if warn, crit := d.diskLevels(); crit != warn {
		t.Fatalf("warn %d critical %d", warn, crit)
	}
}

func TestTheWatchRemovesTheBuildCachesOfAnEndedCardOnly(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	ended, _, edd := worktreeHome(t, d, homeRepo(t))
	l := homeLaunch(t, d, homeRepo(t), "live")
	live, ldd := l.task, l.dd
	for _, dd := range []home.HomeDirs{edd, ldd} {
		cache := filepath.Join(dd.Tmp, "go-build")
		if err := os.MkdirAll(cache, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cache, "x"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	endCard(t, d, ended.ID, store.StatusDone)
	if err := d.st.SetTags(ended.ID, []string{api.ClosedTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.BackdateActivity(ended.ID, WokenGuard+time.Minute); err != nil {
		t.Fatal(err)
	}
	endCard(t, d, live.ID, store.StatusRunning)
	if err := d.st.BackdateActivity(live.ID, WokenGuard+time.Minute); err != nil {
		t.Fatal(err)
	}
	d.reclaimCaches(time.Now())
	if _, err := os.Stat(filepath.Join(edd.Tmp, "go-build")); !os.IsNotExist(err) {
		t.Fatalf("the ended card's cache is left: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ldd.Tmp, "go-build")); err != nil {
		t.Fatalf("a live card's cache was removed: %v", err)
	}
}

func TestTheWatchRemovesOldTempAtriumMadeAndLeavesNewAndForeignTemp(t *testing.T) {
	d := testDaemon(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TEMP", tmp)
	t.Setenv("TMP", tmp)
	old, fresh, foreign := filepath.Join(tmp, "atrium-old"), filepath.Join(tmp, "atrium-new"), filepath.Join(tmp, "other-old")
	inUse := filepath.Join(tmp, "atrium-says")
	for _, p := range []string{old, fresh, foreign, inUse} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(inUse, "card.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-DiskTempAge - time.Hour)
	for _, p := range []string{old, foreign, inUse} {
		if err := os.Chtimes(p, past, past); err != nil {
			t.Fatal(err)
		}
	}
	d.reclaimTemp(time.Now())
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old atrium temp is left: %v", err)
	}
	for _, p := range []string{fresh, foreign, inUse} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s was removed: %v", p, err)
		}
	}
}

func TestTheFreeSpaceButtonRemovesWhatAtriumOwnsAndSaysHowMuch(t *testing.T) {
	d, _ := homeRoom(t)
	ended, _, edd := worktreeHome(t, d, homeRepo(t))
	cache := filepath.Join(edd.Tmp, "go-build")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "x"), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	endCard(t, d, ended.ID, store.StatusDone)
	if err := d.st.SetTags(ended.ID, []string{api.ClosedTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.BackdateActivity(ended.ID, WokenGuard+time.Minute); err != nil {
		t.Fatal(err)
	}
	setLevels(t, d, hugeMB, 1)
	got := d.DiskFree().(map[string]any)
	if got["freed_bytes"].(int64) <= 0 {
		t.Fatalf("answer %+v, wanted bytes freed", got)
	}
	st := got["status"].(DiskStatus)
	if st.Level != DiskWarning || !strings.Contains(st.Say, "already freed") {
		t.Fatalf("status %+v, wanted a warning that says what was freed", st)
	}
}

func TestTheNoticeIsLedByTheRoomNameAndTheLevel(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	if got := d.DiskNotice(); got != "" {
		t.Fatalf("notice %q while fine", got)
	}
	setLevels(t, d, hugeMB, 1)
	d.diskTick(context.Background(), time.Now())
	if got := d.DiskNotice(); !strings.HasPrefix(got, d.RoomWord()+": disk getting low. ") {
		t.Fatalf("warning notice %q", got)
	}
	setLevels(t, d, hugeMB, hugeMB)
	d.diskTick(context.Background(), time.Now())
	if got := d.DiskNotice(); !strings.HasPrefix(got, d.RoomWord()+": disk nearly full. ") {
		t.Fatalf("critical notice %q", got)
	}
	if st := d.DiskState().(DiskStatus); st.Notice != d.DiskNotice() {
		t.Fatalf("state notice %q", st.Notice)
	}
}

func TestTheDefaultLevelsAreTwentyAndTenGigabytes(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	if warn, crit := d.diskLevels(); warn != 20<<30 || crit != 10<<30 {
		t.Fatalf("warn %d critical %d", warn, crit)
	}
}
