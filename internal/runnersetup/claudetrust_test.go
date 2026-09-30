package runnersetup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Claude's folder trust, written at launch. See claudetrust.go.

func claudeHome(t *testing.T) (Env, string) {
	t.Helper()
	home := t.TempDir()
	env := Env{Home: home, Getenv: func(string) string { return "" }}
	path := filepath.Join(home, ".claude.json")
	writeFile(t, path, `{
  "numStartups": 7,
  "oauthAccount": {"emailAddress": "someone@example.com"},
  "projects": {
    "/elsewhere": {"allowedTools": ["Bash"], "hasTrustDialogAccepted": true}
  }
}`)
	return env, path
}

func readClaudeConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err != nil {
		t.Fatalf("%s is not json after the write: %v", path, err)
	}
	return obj
}

func projectTrusted(obj map[string]any, key string) bool {
	projects, _ := obj["projects"].(map[string]any)
	entry, _ := projects[key].(map[string]any)
	return entry["hasTrustDialogAccepted"] == true
}

func keyFor(t *testing.T, dir string) string {
	t.Helper()
	keys := claudeTrustKeys(dir, runtime.GOOS)
	if len(keys) == 0 {
		t.Fatalf("no key for %s", dir)
	}
	return keys[0]
}

// THE BUG. A launch folder claude has never seen is trusted before claude
// starts, and everything else in the file is kept.
func TestALaunchFolderIsTrustedAndTheRestOfTheFileKept(t *testing.T) {
	env, path := claudeHome(t)
	dir := filepath.Join(t.TempDir(), "new-worktree")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	note, err := claudeLaunch(env, dir)
	if err != nil || note == "" {
		t.Fatalf("the launch folder was not trusted: note %q, err %v", note, err)
	}
	obj := readClaudeConfig(t, path)
	if !projectTrusted(obj, keyFor(t, dir)) {
		t.Fatalf("no trust entry for %s in %v", keyFor(t, dir), obj["projects"])
	}
	if obj["numStartups"] != float64(7) || obj["oauthAccount"] == nil {
		t.Fatalf("keys atrium does not own were lost: %v", obj)
	}
	other := obj["projects"].(map[string]any)["/elsewhere"].(map[string]any)
	if other["allowedTools"] == nil || other["hasTrustDialogAccepted"] != true {
		t.Fatalf("another project's entry was changed: %v", other)
	}

	// A second launch into the same folder writes nothing.
	before, _ := os.ReadFile(path)
	note, err = claudeLaunch(env, dir)
	if err != nil || note != "" {
		t.Fatalf("a folder already trusted was written again: note %q, err %v", note, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("the file changed on a launch that had nothing to add")
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("the lock was left behind: %v", err)
	}
}

// An entry claude made for the folder already, untrusted, keeps its fields.
func TestAnExistingUntrustedEntryKeepsItsFields(t *testing.T) {
	env, path := claudeHome(t)
	dir := t.TempDir()
	key := keyFor(t, dir)
	writeFile(t, path, fmt.Sprintf(`{"projects": {%q: {"hasTrustDialogAccepted": false, "lastCost": 3}}}`, key))

	if _, err := claudeLaunch(env, dir); err != nil {
		t.Fatal(err)
	}
	obj := readClaudeConfig(t, path)
	entry := obj["projects"].(map[string]any)[key].(map[string]any)
	if entry["hasTrustDialogAccepted"] != true || entry["lastCost"] != float64(3) {
		t.Fatalf("the entry was not trusted in place: %v", entry)
	}
}

// A claude that has never run has no file, and atrium does not invent one:
// claude's first run writes the sign-in and onboarding state it would lack.
func TestNoConfigFileIsLeftAlone(t *testing.T) {
	env := Env{Home: t.TempDir(), Getenv: func(string) string { return "" }}
	note, err := claudeLaunch(env, t.TempDir())
	if err != nil || note != "" {
		t.Fatalf("note %q, err %v", note, err)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".claude.json")); !os.IsNotExist(err) {
		t.Fatal("atrium created a claude config that did not exist")
	}
}

// A file claude could not read either is not rewritten.
func TestAnUnreadableConfigIsNotRewritten(t *testing.T) {
	env, path := claudeHome(t)
	writeFile(t, path, `{not json`)
	if _, err := claudeLaunch(env, t.TempDir()); err == nil {
		t.Fatal("an unreadable file did not report an error")
	}
	if b, _ := os.ReadFile(path); string(b) != `{not json` {
		t.Fatalf("the unreadable file was rewritten: %s", b)
	}
}

// Claude walks up from the cwd looking for trust, outside a repository all the
// way to the root. Trusting home or a root would trust far more than the launch.
func TestHomeAndAFilesystemRootAreNeverTrusted(t *testing.T) {
	env, path := claudeHome(t)
	before, _ := os.ReadFile(path)
	root := filepath.VolumeName(env.Home) + string(filepath.Separator)
	for _, dir := range []string{env.Home, root} {
		if note, err := claudeLaunch(env, dir); err != nil || note != "" {
			t.Fatalf("%s: note %q, err %v", dir, note, err)
		}
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("home or a filesystem root was written as trusted")
	}
}

// CLAUDE_CONFIG_DIR moves the file, and a legacy .config.json in the config
// directory wins over it, which is the order claude reads them in.
func TestTheConfigPathFollowsClaude(t *testing.T) {
	home := t.TempDir()
	cfg := t.TempDir()
	env := Env{Home: home, Getenv: func(string) string { return "" }}
	if got := claudeConfigPath(env); got != filepath.Join(home, ".claude.json") {
		t.Fatalf("default: %s", got)
	}
	env.RowEnv = map[string]string{"CLAUDE_CONFIG_DIR": cfg}
	if got := claudeConfigPath(env); got != filepath.Join(cfg, ".claude.json") {
		t.Fatalf("with CLAUDE_CONFIG_DIR: %s", got)
	}
	writeFile(t, filepath.Join(cfg, ".config.json"), `{}`)
	if got := claudeConfigPath(env); got != filepath.Join(cfg, ".config.json") {
		t.Fatalf("with a legacy file: %s", got)
	}
}

// Windows keys use forward slashes, which is how claude writes them there.
func TestWindowsKeysUseForwardSlashes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a windows path")
	}
	keys := claudeTrustKeys(`C:\work\tree`, "windows")
	if len(keys) == 0 || keys[0] != "C:/work/tree" {
		t.Fatalf("got %v", keys)
	}
}

// A symlinked folder is written under both names, so claude finds it whichever
// one it resolves. On macOS every temp directory is one.
func TestASymlinkedFolderIsTrustedUnderBothNames(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	keys := claudeTrustKeys(link, runtime.GOOS)
	if len(keys) != 2 {
		t.Fatalf("want the link and its target, got %v", keys)
	}
}

// A SESSION HOLDING CLAUDE'S LOCK IS WAITED FOR, and the entry lands after it
// lets go rather than under it.
func TestAHeldLockIsWaitedFor(t *testing.T) {
	env, path := claudeHome(t)
	dir := t.TempDir()
	lock := path + ".lock"
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(150 * time.Millisecond)
		os.Remove(lock)
		close(released)
	}()
	if _, err := claudeLaunch(env, dir); err != nil {
		t.Fatal(err)
	}
	select {
	case <-released:
	default:
		t.Fatal("the write went ahead while the lock was held")
	}
	if !projectTrusted(readClaudeConfig(t, path), keyFor(t, dir)) {
		t.Fatal("the entry never landed")
	}
}

// A lock nobody refreshed for longer than claude's stale limit belongs to a
// session that died holding it, and proper-lockfile would take it over too.
func TestAStaleLockIsTakenOver(t *testing.T) {
	env, path := claudeHome(t)
	lock := path + ".lock"
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * claudeLockStale)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := claudeLaunch(env, dir); err != nil {
		t.Fatal(err)
	}
	if !projectTrusted(readClaudeConfig(t, path), keyFor(t, dir)) {
		t.Fatal("a stale lock stopped the write")
	}
}

// A lock that stays held is given up on, and the file is left alone. The
// launch goes ahead and claude's own dialog is the fallback.
func TestALockThatStaysHeldGivesUp(t *testing.T) {
	env, path := claudeHome(t)
	if err := os.Mkdir(path+".lock", 0o700); err != nil {
		t.Fatal(err)
	}
	was := claudeLockWait
	claudeLockWait = 100 * time.Millisecond
	t.Cleanup(func() { claudeLockWait = was })
	before, _ := os.ReadFile(path)
	if _, err := claudeLaunch(env, t.TempDir()); err == nil {
		t.Fatal("a held lock did not report an error")
	}
	if after, _ := os.ReadFile(path); string(before) != string(after) {
		t.Fatal("the file was written without the lock")
	}
}

// CONCURRENT WITH CLAUDE. Sessions writing the file the way claude does (take
// the lock, re-read, change one key, write) and launches trusting folders at
// the same time: every change survives.
func TestConcurrentClaudeWritesKeepEveryChange(t *testing.T) {
	env, path := claudeHome(t)
	const n = 8
	var dirs []string
	for i := 0; i < n; i++ {
		dirs = append(dirs, t.TempDir())
	}
	claudeWrite := func() error {
		unlock, err := lockClaudeConfig(path)
		if err != nil {
			return err
		}
		defer unlock()
		obj, raw, _, err := readJSONObject(path)
		if err != nil {
			return err
		}
		var count int
		_ = json.Unmarshal(obj["numStartups"], &count)
		obj["numStartups"] = json.RawMessage(fmt.Sprint(count + 1))
		_, err = writeJSONFile(path, obj, raw)
		return err
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2*n)
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(dir string) { defer wg.Done(); _, err := claudeLaunch(env, dir); errs <- err }(dirs[i])
		go func() { defer wg.Done(); errs <- claudeWrite() }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	obj := readClaudeConfig(t, path)
	if obj["numStartups"] != float64(7+n) {
		t.Fatalf("a claude write was lost: numStartups %v", obj["numStartups"])
	}
	for _, dir := range dirs {
		if !projectTrusted(obj, keyFor(t, dir)) {
			t.Fatalf("a trust entry was lost: %s", dir)
		}
	}
}

// A Mkdir answered ACCESS_DENIED while the lock is being released (Windows,
// delete pending) is waited out, and the lock is then taken.
func TestLockRetriesADeniedMkdir(t *testing.T) {
	_, path := claudeHome(t)
	real := mkdirLock
	denied := 2
	mkdirLock = func(name string, perm os.FileMode) error {
		if denied > 0 {
			denied--
			return &os.PathError{Op: "mkdir", Path: name, Err: os.ErrPermission}
		}
		return real(name, perm)
	}
	t.Cleanup(func() { mkdirLock = real })
	unlock, err := lockClaudeConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if denied != 0 {
		t.Fatalf("the denied answers were not retried: %d left", denied)
	}
	if exists(path + ".lock") {
		t.Fatal("unlock left the lock directory")
	}
}

// A Mkdir that stays denied is a real permission error, reported as itself
// when the wait ends rather than as a session holding the lock.
func TestLockReportsAPersistentDeniedMkdir(t *testing.T) {
	_, path := claudeHome(t)
	real := mkdirLock
	mkdirLock = func(name string, perm os.FileMode) error {
		return &os.PathError{Op: "mkdir", Path: name, Err: os.ErrPermission}
	}
	was := claudeLockWait
	claudeLockWait = 100 * time.Millisecond
	t.Cleanup(func() { mkdirLock = real; claudeLockWait = was })
	if _, err := lockClaudeConfig(path); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("got %v, want the permission error", err)
	}
}
