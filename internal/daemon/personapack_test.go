package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// git runs a git command for a test's own setup. The TEST writes; the code
// under test never does.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// packRepo is a checkout shaped like dotagents: a pack under `personas/` and
// something else beside it. Returns the checkout and the pack directory.
func packRepo(t *testing.T) (string, string) {
	t.Helper()
	needGit(t)
	repo := t.TempDir()
	git(t, repo, "init", "-q")
	pack := filepath.Join(repo, "personas")
	write(t, filepath.Join(pack, "README.md"), "pack\n")
	write(t, filepath.Join(pack, "alpha", "memory", "MEMORY.md"), "one\n")
	write(t, filepath.Join(pack, "beta", "persona.md"), "beta\n")
	write(t, filepath.Join(repo, "other", "notes.md"), "not the pack\n")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "seed")
	return repo, pack
}

func TestUncommittedPackFilesAreCountedAndNamedByPersona(t *testing.T) {
	repo, pack := packRepo(t)
	write(t, filepath.Join(pack, "alpha", "memory", "MEMORY.md"), "two\n")
	write(t, filepath.Join(pack, "gamma", "memory", "new lesson.md"), "new\n")
	write(t, filepath.Join(pack, "README.md"), "pack, edited\n")
	// Outside the pack, so it is not the nag's business.
	write(t, filepath.Join(repo, "other", "notes.md"), "edited\n")

	r, err := readPack(context.Background(), pack)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Changed) != 3 {
		t.Fatalf("changed %q, want the three files under the pack", r.Changed)
	}
	if want := []string{"alpha", "gamma"}; !reflect.DeepEqual(r.Personas, want) {
		t.Fatalf("personas %q, want %q", r.Personas, want)
	}
	v := packViewFor(pack, r, nil, nil, time.Now())
	if !v.Nag || !strings.Contains(v.Text, "3 files not committed") {
		t.Fatalf("text %q, want 3 files not committed", v.Text)
	}
	if !strings.Contains(v.Detail, "alpha, gamma") || !strings.Contains(v.Detail, "/safe-to-push") {
		t.Fatalf("detail %q, want the personas and /safe-to-push", v.Detail)
	}
}

func TestNoUpstreamSaysSoInsteadOfACount(t *testing.T) {
	_, pack := packRepo(t)
	r, err := readPack(context.Background(), pack)
	if err != nil {
		t.Fatal(err)
	}
	if !r.NoUpstream || r.Unpushed != 0 {
		t.Fatalf("read %+v, want no upstream and no count", r)
	}
	v := packViewFor(pack, r, nil, nil, time.Now())
	if v.Text != "persona pack: no upstream configured" {
		t.Fatalf("text %q", v.Text)
	}
}

func TestUnpushedCommitsAreCounted(t *testing.T) {
	needGit(t)
	remote := t.TempDir()
	git(t, remote, "init", "-q", "--bare")
	_, pack := packRepo(t)
	repo := filepath.Dir(pack)
	git(t, repo, "remote", "add", "origin", remote)
	git(t, repo, "push", "-q", "-u", "origin", "HEAD")

	r, err := readPack(context.Background(), pack)
	if err != nil {
		t.Fatal(err)
	}
	if r.NoUpstream || r.Unpushed != 0 || len(r.Changed) != 0 {
		t.Fatalf("read %+v, want a clean pushed pack", r)
	}
	if v := packViewFor(pack, r, nil, nil, time.Now()); v.Nag {
		t.Fatalf("a clean pushed pack nags: %+v", v)
	}

	for _, s := range []string{"a", "b"} {
		write(t, filepath.Join(pack, "alpha", s+".md"), s)
		git(t, repo, "add", "-A")
		git(t, repo, "commit", "-q", "-m", s)
	}
	r, err = readPack(context.Background(), pack)
	if err != nil {
		t.Fatal(err)
	}
	if r.Unpushed != 2 {
		t.Fatalf("unpushed %d, want 2", r.Unpushed)
	}
	if v := packViewFor(pack, r, nil, nil, time.Now()); v.Text != "persona pack: 2 commits not pushed" {
		t.Fatalf("text %q", v.Text)
	}
}

func TestADirectoryThatIsNotACheckoutIsAReasonNotAFailure(t *testing.T) {
	needGit(t)
	dir := t.TempDir()
	// Stops git searching above the temp directory, in case it sits in one.
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	_, err := readPack(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "not in a git checkout") {
		t.Fatalf("err %v, want not in a git checkout", err)
	}
	v := packViewFor(dir, packRead{}, err, nil, time.Now())
	if v.Nag || v.Count != 0 || !strings.HasPrefix(v.Text, "cannot read the persona pack: ") {
		t.Fatalf("view %+v, want a reason that does not ring", v)
	}
}

func TestAMissingPathIsAReason(t *testing.T) {
	_, err := readPack(context.Background(), filepath.Join(t.TempDir(), "gone"))
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err %v", err)
	}
}

func TestAMissingGitIsAReason(t *testing.T) {
	_, pack := packRepo(t)
	old := packGit
	packGit = "atrium-no-such-git-binary"
	t.Cleanup(func() { packGit = old })
	_, err := readPack(context.Background(), pack)
	if err == nil || !strings.Contains(err.Error(), "git is not installed") {
		t.Fatalf("err %v", err)
	}
}

func TestAGitThatDoesNotAnswerTimesOut(t *testing.T) {
	_, pack := packRepo(t)
	old := PersonaPackTimeout
	PersonaPackTimeout = time.Nanosecond
	t.Cleanup(func() { PersonaPackTimeout = old })
	_, err := readPack(context.Background(), pack)
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("err %v", err)
	}
}

// Only the two reads can run. A name that is not on the list is refused before
// anything is started.
func TestOnlyTheAllowListedReadsRun(t *testing.T) {
	if len(packReads) != 2 {
		t.Fatalf("packReads has %d commands, want the two reads", len(packReads))
	}
	for _, args := range packReads {
		switch args[0] {
		case "status", "rev-list":
		default:
			t.Fatalf("%q is on the allow-list and is not a read", args[0])
		}
	}
	if _, err := packGitRead(context.Background(), t.TempDir(), "push"); err == nil {
		t.Fatal("a name that is not on the allow-list ran")
	}
}

// `git status` refreshes the index when it can, which is a write. The nag runs
// it with optional locks off, so the index is left exactly as it was.
func TestReadingThePackDoesNotWriteTheIndex(t *testing.T) {
	repo, pack := packRepo(t)
	index := filepath.Join(repo, ".git", "index")
	// A touched file with the same content is what makes status want to
	// refresh the index.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(pack, "beta", "persona.md"), later, later); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readPack(context.Background(), pack); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("reading the pack rewrote the index")
	}
}

// The nag rings on the stuck-agent backoff from when this state was first seen,
// and starts over when the state changes.
func TestTheNagEscalatesOnTheBackoffAndResetsWhenTheStateChanges(t *testing.T) {
	start := time.Now()
	r := packRead{Changed: []string{" M personas/alpha/x.md"}, Personas: []string{"alpha"}, NoUpstream: true}
	v := packViewFor("p", r, nil, nil, start)
	if v.Count != 0 {
		t.Fatalf("count %d at first sight", v.Count)
	}
	for _, c := range []struct {
		after time.Duration
		count int
	}{{time.Minute, 1}, {2 * time.Minute, 2}, {5 * time.Minute, 3}, {10 * time.Minute, 4}, {time.Hour, 6}} {
		v = packViewFor("p", r, nil, v, start.Add(c.after))
		if v.Count != c.count {
			t.Fatalf("after %s count %d, want %d", c.after, v.Count, c.count)
		}
	}
	key := v.Key
	r.Changed = append(r.Changed, "?? personas/alpha/y.md")
	v = packViewFor("p", r, nil, v, start.Add(2*time.Hour))
	if v.Key == key || v.Count != 0 || !v.Since.Equal(start.Add(2*time.Hour)) {
		t.Fatalf("view %+v, want a new key starting over", v)
	}
}

// Through the daemon: off until the path is set, then the settings answer
// carries what the timer found.
func TestTheSettingsAnswerCarriesThePackNag(t *testing.T) {
	_, pack := packRepo(t)
	write(t, filepath.Join(pack, "alpha", "memory", "MEMORY.md"), "changed\n")
	d := testDaemon(t)

	get := func() map[string]any {
		rec := httptest.NewRecorder()
		d.ap.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/settings", nil))
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%v: %s", err, rec.Body.String())
		}
		return out
	}
	d.checkPack(context.Background(), time.Now())
	if s := get(); s["persona_pack"] != nil || s["persona_pack_path"] != "" {
		t.Fatalf("the nag is on with no path: %v %v", s["persona_pack"], s["persona_pack_path"])
	}

	body, _ := json.Marshal(map[string]string{"persona_pack_path": "  " + pack + "  "})
	rec := httptest.NewRecorder()
	d.ap.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/settings",
		strings.NewReader(string(body))))
	if rec.Code != http.StatusOK {
		t.Fatalf("saving the path answered %d: %s", rec.Code, rec.Body.String())
	}
	if got, _ := d.st.Setting(store.SettingPersonaPackPath); got != pack {
		t.Fatalf("stored %q, want %q", got, pack)
	}
	select {
	case <-d.pack.nudge:
	default:
		t.Fatal("saving the path did not ask for a look now")
	}
	d.checkPack(context.Background(), time.Now())
	p, _ := get()["persona_pack"].(map[string]any)
	if p == nil || p["uncommitted"] != float64(1) || p["no_upstream"] != true || p["nag"] != true {
		t.Fatalf("persona_pack %v", p)
	}
}
