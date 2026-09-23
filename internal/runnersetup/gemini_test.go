package runnersetup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// geminiHome is a temp HOME with a workspace root inside it, and no process
// environment leaking in.
func geminiHome(t *testing.T) (Env, string) {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, "worktrees")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return Env{Home: home, Roots: []string{root}, Getenv: func(string) string { return "" }}, root
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func trustFile(env Env) string { return filepath.Join(env.Home, ".gemini", "trustedFolders.json") }

func readTrust(t *testing.T, env Env) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(trustFile(env))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestGeminiTrustFailsForAnUntrustedRootAndOffersIt(t *testing.T) {
	env, root := geminiHome(t)
	r := geminiTrustCheck(env)
	if r.State != Fail || r.Fix != FixApply || len(r.Targets) != 1 || r.Targets[0] != root {
		t.Fatalf("an untrusted root should be a fail with an apply fix targeting it, got %+v", r)
	}
}

func TestGeminiTrustApplyTrustsTheRootAndKeepsOtherEntries(t *testing.T) {
	env, root := geminiHome(t)
	writeFile(t, trustFile(env), `{"c:/elsewhere": "DO_NOT_TRUST"}`)
	res, err := Fix(Gemini, env, "trust", root)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Backup == "" {
		t.Fatalf("a fix over an existing file should change it and keep a backup, got %+v", res)
	}
	m := readTrust(t, env)
	if m["c:/elsewhere"] != doNotTrust {
		t.Fatalf("an entry atrium did not add was changed: %v", m)
	}
	if r := geminiTrustCheck(env); r.State != OK {
		t.Fatalf("after the fix the root should be trusted, got %+v", r)
	}
	if !exists(trustFile(env)+backupOriginal) || !exists(trustFile(env)+backupLast) {
		t.Fatal("both backups should exist after the first change")
	}
}

func TestGeminiTrustOriginalBackupIsNeverOverwritten(t *testing.T) {
	env, root := geminiHome(t)
	writeFile(t, trustFile(env), `{}`)
	if _, err := addTrust(env, filepath.Join(root, "a")); err != nil {
		t.Fatal(err)
	}
	if _, err := addTrust(env, filepath.Join(root, "b")); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.ReadFile(trustFile(env) + backupOriginal)
	if strings.TrimSpace(string(orig)) != "{}" {
		t.Fatalf("the original backup should be the file before atrium's first change, got %s", orig)
	}
	last, _ := os.ReadFile(trustFile(env) + backupLast)
	if !strings.Contains(string(last), "/a") || strings.Contains(string(last), "/b") {
		t.Fatalf("the last backup should be the file before the most recent change, got %s", last)
	}
}

func TestGeminiTrustParentCoversTheRoot(t *testing.T) {
	env, root := geminiHome(t)
	// TRUST_PARENT on a child of home covers home, and so the root under it.
	writeFile(t, trustFile(env), `{"`+filepath.ToSlash(filepath.Join(env.Home, "x"))+`": "TRUST_PARENT"}`)
	if r := geminiTrustCheck(env); r.State != OK {
		t.Fatalf("a TRUST_PARENT on a sibling should cover the root, got %+v", r)
	}
	_ = root
}

func TestGeminiTrustDoNotTrustIsExplainedNotApplied(t *testing.T) {
	env, root := geminiHome(t)
	writeFile(t, trustFile(env), `{"`+filepath.ToSlash(root)+`": "DO_NOT_TRUST"}`)
	r := geminiTrustCheck(env)
	if r.State != Fail || r.Fix != FixExplain {
		t.Fatalf("a DO_NOT_TRUST root should be explained, got %+v", r)
	}
	if _, err := Fix(Gemini, env, "trust", root); err == nil {
		t.Fatal("a fix over a DO_NOT_TRUST rule should be refused")
	}
}

func TestGeminiTrustRefusesAFileGeminiWouldRefuse(t *testing.T) {
	env, root := geminiHome(t)
	writeFile(t, trustFile(env), `{"c:/x": "MAYBE"}`)
	if r := geminiTrustCheck(env); r.State != Fail || r.Fix != FixExplain {
		t.Fatalf("an invalid trust level should be explained, got %+v", r)
	}
	if _, err := addTrust(env, root); err == nil {
		t.Fatal("atrium wrote into a file gemini refuses")
	}
}

func TestGeminiTrustOkWhenFolderTrustIsOff(t *testing.T) {
	env, _ := geminiHome(t)
	writeFile(t, filepath.Join(env.Home, ".gemini", "settings.json"),
		`{"security":{"folderTrust":{"enabled":false}}}`)
	if r := geminiTrustCheck(env); r.State != OK {
		t.Fatalf("folder trust off should read ok, got %+v", r)
	}
}

func TestGeminiTrustHonoursTheEnvironment(t *testing.T) {
	env, _ := geminiHome(t)
	env.RowEnv = map[string]string{"GEMINI_CLI_TRUST_WORKSPACE": "true"}
	if r := geminiTrustCheck(env); r.State != OK {
		t.Fatalf("GEMINI_CLI_TRUST_WORKSPACE=true should read ok, got %+v", r)
	}
	env.RowEnv = map[string]string{"GEMINI_RESTRICTED_MODE": "true"}
	if r := geminiTrustCheck(env); r.State != Fail || r.Fix != FixExplain {
		t.Fatalf("restricted mode should be explained, got %+v", r)
	}
}

func TestGeminiTrustWarnsWithNoRoots(t *testing.T) {
	env, _ := geminiHome(t)
	env.Roots = nil
	if r := geminiTrustCheck(env); r.State != Warn {
		t.Fatalf("no roots should warn, got %+v", r)
	}
}

func TestGeminiTrustFollowsGeminiCLIHome(t *testing.T) {
	env, root := geminiHome(t)
	other := t.TempDir()
	env.RowEnv = map[string]string{"GEMINI_CLI_HOME": other}
	if _, err := Fix(Gemini, env, "trust", root); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(other, ".gemini", "trustedFolders.json")) {
		t.Fatal("GEMINI_CLI_HOME should move the file atrium writes")
	}
	if exists(trustFile(env)) {
		t.Fatal("the default file was written although GEMINI_CLI_HOME moved it")
	}
}

func TestGeminiLaunchTrustsAWorktreeInsideARoot(t *testing.T) {
	env, root := geminiHome(t)
	wt := filepath.Join(root, "repo", "branch")
	os.MkdirAll(wt, 0o700)
	note, err := geminiLaunch(env, wt)
	if err != nil || note == "" {
		t.Fatalf("a launch inside a root should trust the folder, note %q err %v", note, err)
	}
	if trustLevel(readTrust(t, env), wt, env.goos()) != trustFolder {
		t.Fatal("the worktree is not trusted after launch")
	}
	// Again: already decided, so nothing is written.
	if note, _ := geminiLaunch(env, wt); note != "" {
		t.Fatalf("a second launch wrote again: %q", note)
	}
}

func TestGeminiLaunchLeavesOutsideFoldersAndNosAlone(t *testing.T) {
	env, root := geminiHome(t)
	outside := t.TempDir()
	if note, _ := geminiLaunch(env, outside); note != "" || exists(trustFile(env)) {
		t.Fatal("a launch outside every root must write nothing")
	}
	wt := filepath.Join(root, "no")
	os.MkdirAll(wt, 0o700)
	writeFile(t, trustFile(env), `{"`+filepath.ToSlash(wt)+`": "DO_NOT_TRUST"}`)
	if note, _ := geminiLaunch(env, wt); note != "" {
		t.Fatal("a launch overrode a DO_NOT_TRUST rule")
	}
	if len(readTrust(t, env)) != 1 {
		t.Fatal("a launch into a denied folder added a rule")
	}
}

func TestGeminiAuthStates(t *testing.T) {
	env, _ := geminiHome(t)
	settings := filepath.Join(env.Home, ".gemini", "settings.json")

	if r := geminiAuthCheck(env); r.State != Fail || r.Command == "" {
		t.Fatalf("no sign-in chosen should fail with a command, got %+v", r)
	}

	writeFile(t, settings, `{"security":{"auth":{"selectedType":"gemini-api-key"}}}`)
	if r := geminiAuthCheck(env); r.State != Warn || r.Fix != FixExplain {
		t.Fatalf("a key atrium cannot see should warn, got %+v", r)
	}
	writeFile(t, filepath.Join(env.Home, ".gemini", ".env"), "# comment\nGEMINI_API_KEY=abc\n")
	if r := geminiAuthCheck(env); r.State != OK {
		t.Fatalf("a key in ~/.gemini/.env should be ok, got %+v", r)
	}
	env.RowEnv = map[string]string{"GEMINI_API_KEY": "abc"}
	r := geminiAuthCheck(env)
	if r.State != Warn || !strings.Contains(r.Detail, "does not hold credentials") {
		t.Fatalf("a key in the harness env should warn, got %+v", r)
	}
	if strings.Contains(r.Detail+r.Command, "abc") {
		t.Fatal("the key's value leaked into the report")
	}
	env.RowEnv = nil

	writeFile(t, settings, `{"security":{"auth":{"selectedType":"oauth-personal"}}}`)
	if r := geminiAuthCheck(env); r.State != Fail {
		t.Fatalf("oauth with no saved sign-in should fail, got %+v", r)
	}
	writeFile(t, filepath.Join(env.Home, ".gemini", "oauth_creds.json"), `{}`)
	if r := geminiAuthCheck(env); r.State != OK {
		t.Fatalf("oauth with a saved sign-in should be ok, got %+v", r)
	}

	writeFile(t, settings, `{"security":{"auth":{"selectedType":"vertex-ai"}}}`)
	if r := geminiAuthCheck(env); r.State != NA {
		t.Fatalf("vertex should be n/a, got %+v", r)
	}
}

func TestAuthIsNeverApplied(t *testing.T) {
	env, _ := geminiHome(t)
	if _, err := Fix(Gemini, env, "auth", ""); err != ErrExplainOnly {
		t.Fatalf("sign-in must be explain-only, got %v", err)
	}
}

func TestFixRefusesATargetTheCheckDidNotOffer(t *testing.T) {
	env, _ := geminiHome(t)
	if _, err := Fix(Gemini, env, "trust", t.TempDir()); err != ErrNothingToFix {
		t.Fatalf("an arbitrary target should be refused, got %v", err)
	}
	if exists(trustFile(env)) {
		t.Fatal("a refused fix wrote the file")
	}
}

func TestForMatchesOnTheCommandLeaf(t *testing.T) {
	for cmd, want := range map[string]*Adapter{
		"gemini": Gemini, `C:\Users\x\AppData\Roaming\npm\gemini.cmd`: Gemini, "claude": Claude,
		"codex": nil, "ollama": nil,
	} {
		if got := For(&store.Harness{Cmd: cmd}); got != want {
			t.Errorf("For(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestWorkspaceRootsSkipsDisabledAndWorktreesOff(t *testing.T) {
	got := WorkspaceRoots([]*store.Provider{
		{Root: "D:/git/github", Enabled: true, Worktrees: true, WorktreeRoot: "D:/worktrees"},
		{Root: "E:/off", Enabled: false},
		{Root: "F:/src", Enabled: true, WorktreeRoot: "F:/ignored"},
	})
	want := []string{"D:/git/github", "D:/worktrees", "F:/src"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}
