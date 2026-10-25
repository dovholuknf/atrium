//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/cardroot"
	"github.com/dovholuknf/atrium/internal/store"
)

func probeReq(t *testing.T, id string, paths []string) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]any{"paths": paths})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/v1/tasks/"+id+"/files/probe", strings.NewReader(string(body)))
	r.SetPathValue("id", id)
	return r
}

// probed returns the answer as rel-by-candidate, which is the shape the board
// uses: it matches what came back against the token still on screen.
func probed(t *testing.T, s *Server, id string, paths []string) map[string]probeHit {
	t.Helper()
	w := httptest.NewRecorder()
	s.probeFiles(w, probeReq(t, id, paths))
	if w.Code != http.StatusOK {
		t.Fatalf("probe answered %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Found []probeHit `json:"found"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	out := map[string]probeHit{}
	for _, h := range body.Found {
		out[h.Path] = h
	}
	return out
}

func TestAProbeAnswersOnlyForWhatIsThere(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	if err := os.WriteFile(filepath.Join(work, "notes.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(work, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}

	// The second half of this list is the reason the endpoint exists: every
	// one of them is shaped like a filename on a terminal line and none of
	// them is a file.
	got := probed(t, s, task.ID, []string{
		"notes.md", "internal",
		"v2.1.263", "zrok.io", "foo.bar", "Opus", "README.md",
	})

	if len(got) != 2 {
		t.Fatalf("%d of 7 candidates came back as files: %v", len(got), got)
	}
	if h := got["notes.md"]; h.Rel != "notes.md" || h.Dir || h.Size != 5 {
		t.Fatalf("the file came back as %+v", h)
	}
	if h := got["internal"]; h.Rel != "internal" || !h.Dir {
		t.Fatalf("the directory came back as %+v", h)
	}
}

// A full path is what a runner prints, far more often than a relative one, so
// it has to work. It works ONLY while it lands inside the card.
func TestAProbeTakesAFullPathInsideTheCard(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	if err := os.WriteFile(filepath.Join(work, "out.log"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(work, "out.log")

	got := probed(t, s, task.ID, []string{full})
	if h, ok := got[full]; !ok || h.Rel != "out.log" {
		t.Fatalf("a full path inside the card did not come back: %v", got)
	}
}

// The refusal that matters. A path that resolves outside the worktree is
// simply absent, which is the same answer as one that is not there, so the
// board learns nothing about the machine it is not already allowed to see.
func TestAProbeNeverLeavesTheCard(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	// Real, readable, and outside. The traversal has something to find, so a
	// missing answer means it was refused rather than that it was absent.
	outside := filepath.Join(filepath.Dir(work), "secrets.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(outside) })

	got := probed(t, s, task.ID, []string{
		"../secrets.txt",
		outside,
		filepath.Join(work, "..", "secrets.txt"),
		"/etc/passwd",
		`C:\Windows\System32\drivers\etc\hosts`,
	})
	if len(got) != 0 {
		t.Fatalf("a path outside the card came back: %v", got)
	}
}

// A symlink is a path that resolves somewhere else, so looking at the text of
// the candidate cannot catch it. Refusing has to happen after the link is
// followed, or one `ln -s` inside a worktree reads the whole disk.
func TestAProbeRefusesASymlinkOutOfTheCard(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	outside := filepath.Join(filepath.Dir(work), "elsewhere.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(outside) })

	link := filepath.Join(work, "shortcut.txt")
	if err := os.Symlink(outside, link); err != nil {
		// Windows without developer mode refuses to make one at all, and a
		// test that cannot build the hazard cannot say anything about it.
		t.Skipf("symlinks are not available here: %v", err)
	}

	if got := probed(t, s, task.ID, []string{"shortcut.txt"}); len(got) != 0 {
		t.Fatalf("a symlink pointing out of the card came back: %v", got)
	}
}

// The batch is bounded, and going over it is not an error. The board sends
// what is on a line, and a line it miscounted should cost a truncated answer
// rather than a failed request that leaves nothing underlined.
func TestAProbeCapsTheBatchRatherThanRefusingIt(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	var paths []string
	for i := 0; i < maxProbe*2; i++ {
		name := "f" + strconv.Itoa(i) + ".txt"
		if err := os.WriteFile(filepath.Join(work, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, name)
	}

	got := probed(t, s, task.ID, paths)
	if len(got) != maxProbe {
		t.Fatalf("%d answers for %d candidates, expected the cap of %d",
			len(got), len(paths), maxProbe)
	}
}

// A candidate longer than the cap is not a path somebody is going to click, it
// is a line of output that happened to have no spaces in it.
//
// The over-long candidate RESOLVES TO A REAL FILE, which is the only way the
// test says anything: an absent answer for something that is not there would
// be absent for either reason. Padded with `./` rather than with a long name,
// because Windows will not create a name past 255 characters and the cap being
// tested is on the whole path.
func TestAProbeIgnoresACandidateLongerThanTheCap(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	if err := os.WriteFile(filepath.Join(work, "notes.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	short := "./notes.md"
	long := strings.Repeat("./", maxProbePath) + "notes.md"

	// The same file, twice, so the only difference between the two answers is
	// the length of the way it was written.
	got := probed(t, s, task.ID, []string{short, long})
	if _, ok := got[short]; !ok {
		t.Fatalf("the short spelling of an existing file did not come back: %v", got)
	}
	if _, ok := got[long]; ok {
		t.Fatalf("an over-long candidate was answered: %v", got)
	}
}

// A card with no directory has nothing to measure a candidate against, and
// saying so is better than answering about some other directory.
func TestProbingACardWithNoDirectoryIsRefused(t *testing.T) {
	t.Parallel()
	s, st, _ := fileServer(t)
	task, _, err := st.Register(store.Observed{WireName: "nodir", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	s.probeFiles(w, probeReq(t, task.ID, []string{"notes.md"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("answered %d: %s", w.Code, w.Body.String())
	}
}

func writeIn(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func viewCode(s *Server, id, p string) int {
	w := httptest.NewRecorder()
	s.viewFile(w, viewReq(id, p))
	return w.Code
}

func TestAPathOutsideTheCardOpensOnlyUnderAnOpenRoot(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real server with a database")
	}
	t.Parallel()
	s, st, work := fileServer(t)
	mine := filepath.Join(work, "mine")
	other := filepath.Join(work, "other")
	cards := filepath.Join(work, "cards")
	allowed := filepath.Join(work, "allowed")
	nowhere := filepath.Join(work, "nowhere")
	for _, d := range []string{mine, other, cards, allowed, nowhere} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	task := cardIn(t, st, mine)
	if _, _, err := st.Register(store.Observed{
		WireName: "elsewhere", Worktree: filepath.ToSlash(other), Runner: "claude",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(cardroot.Setting, cards); err != nil {
		t.Fatal(err)
	}

	inCard := writeIn(t, mine, "in.md")
	inOther := writeIn(t, filepath.Join(other, "docs"), "other.md")
	inCards := writeIn(t, filepath.Join(cards, "x"), "root.md")
	inAllowed := writeIn(t, allowed, "listed.md")
	inNowhere := writeIn(t, nowhere, "secret.md")

	for _, p := range []string{inCard, inOther, inCards} {
		if c := viewCode(s, task.ID, p); c != http.StatusOK {
			t.Errorf("%s answered %d, want 200", p, c)
		}
	}
	for _, p := range []string{inAllowed, inNowhere} {
		if c := viewCode(s, task.ID, p); c != http.StatusForbidden {
			t.Errorf("%s answered %d before it was allowed, want 403", p, c)
		}
	}

	if err := st.SetSetting(SettingOpenPaths, filepath.ToSlash(allowed)); err != nil {
		t.Fatal(err)
	}
	if c := viewCode(s, task.ID, inAllowed); c != http.StatusOK {
		t.Errorf("a path under an allowlisted root answered %d, want 200", c)
	}
	if c := viewCode(s, task.ID, inNowhere); c != http.StatusForbidden {
		t.Errorf("a path outside everything answered %d, want 403", c)
	}

	got := probed(t, s, task.ID, []string{inCard, inOther, inAllowed, inNowhere})
	if h := got[inCard]; h.Rel != "in.md" || h.Outside {
		t.Errorf("the card's own file came back as %+v", h)
	}
	if h := got[inOther]; !h.Outside || h.Rel != filepath.ToSlash(inOther) {
		t.Errorf("another card's file came back as %+v", h)
	}
	if h := got[inAllowed]; !h.Outside || h.Size != 5 {
		t.Errorf("an allowlisted file came back as %+v", h)
	}
	if _, ok := got[inNowhere]; ok {
		t.Errorf("a path outside everything was found")
	}
}

func TestADotDotOrASymlinkCannotLeaveAnOpenRoot(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real server with a database")
	}
	t.Parallel()
	s, st, work := fileServer(t)
	mine := filepath.Join(work, "mine")
	allowed := filepath.Join(work, "allowed")
	nowhere := filepath.Join(work, "nowhere")
	for _, d := range []string{mine, allowed} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	task := cardIn(t, st, mine)
	if err := st.SetSetting(SettingOpenPaths, filepath.ToSlash(allowed)); err != nil {
		t.Fatal(err)
	}
	secret := writeIn(t, nowhere, "secret.md")

	if c := viewCode(s, task.ID, filepath.Join(allowed, "..", "nowhere", "secret.md")); c != http.StatusForbidden {
		t.Errorf("a .. out of an allowlisted root answered %d, want 403", c)
	}
	if err := os.Symlink(nowhere, filepath.Join(allowed, "link")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if c := viewCode(s, task.ID, filepath.Join(allowed, "link", "secret.md")); c != http.StatusForbidden {
		t.Errorf("a symlink out of an allowlisted root answered %d, want 403", c)
	}
	if got := probed(t, s, task.ID, []string{filepath.Join(allowed, "link", "secret.md")}); len(got) != 0 {
		t.Errorf("a symlink out of an allowlisted root was found: %v", got)
	}
	if c := viewCode(s, task.ID, secret); c != http.StatusForbidden {
		t.Errorf("the target itself answered %d, want 403", c)
	}
}
