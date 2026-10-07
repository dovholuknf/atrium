package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/store"
)

// A recogniser's fetch is a command, so testing one means running a command.
// The same helper the source tests use stands in for it, re-executed with a
// marker in the environment. See `helperSource` for why the payload goes in a
// file rather than in the environment.
func helperFetch(t *testing.T, r store.Recogniser, out string, fails bool) store.Recogniser {
	t.Helper()
	t.Setenv("ATRIUM_TEST_SOURCE", "1")
	path := filepath.Join(t.TempDir(), "facts")
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_TEST_SOURCE_OUT_FILE", path)
	if fails {
		t.Setenv("ATRIUM_TEST_SOURCE_FAIL", "1")
		t.Setenv("ATRIUM_TEST_SOURCE_ERR", "gh: not logged in")
	}
	r.Fetch = os.Args[0]
	r.FetchArgs = []string{"-test.run=TestHelperSourceProcess"}
	return r
}

func prRow(cwd string) store.Recogniser {
	return store.Recogniser{
		ID: "github-pr", Label: "github pull request", Enabled: true, Rank: 10,
		Pattern: `^https?://(?P<host>github\.com)/(?P<org>[^/]+)/(?P<repo>[^/]+)/pull/(?P<num>\d+)`,
		Kind:    "github",
		Title:   "{org}/{repo}#{num} {title}",
		Tags:    "pull-request,{repo}",
		Cwd:     cwd,
		Window:  "pull-requests",
		Prompt:  "review {url}",
	}
}

// The whole feature in one test: paste a pull request URL, get a launch dialog
// that knows the repo, the org, the host, the title and the first instruction.
func TestAPullRequestURLFillsInADialog(t *testing.T) {
	d := testDaemon(t)
	dir := t.TempDir()
	if _, err := d.st.SaveRecogniser(prRow(filepath.ToSlash(dir))); err != nil {
		t.Fatal(err)
	}

	got, err := d.Recognise("https://github.com/openziti/ziti/pull/4211")
	if err != nil {
		t.Fatal(err)
	}
	if got.Org != "openziti" || got.Repo != "ziti" || got.Host != "github.com" {
		t.Fatalf("the card's own fields are empty: %+v", got)
	}
	if got.Window != "pull-requests" || got.Kind != "github" {
		t.Fatalf("window and kind: %q %q", got.Window, got.Kind)
	}
	if got.Prompt != "review https://github.com/openziti/ziti/pull/4211" {
		t.Fatalf("prompt: %q", got.Prompt)
	}
	if !got.CwdExists {
		t.Fatalf("a directory that is there was reported as missing: %s", got.Problem)
	}
	if got.Problem != "" {
		t.Fatalf("nothing is wrong here, but it said: %s", got.Problem)
	}
}

// ATRIUM DOES NOT MAKE THE WORKTREE. This is the line the whole design rests
// on: a tool that knew git would clone here, and it would be a second, worse
// implementation of something already on the PATH.
func TestAMissingWorktreeIsSaidRatherThanCreated(t *testing.T) {
	d := testDaemon(t)
	missing := filepath.ToSlash(filepath.Join(t.TempDir(), "not-here"))
	if _, err := d.st.SaveRecogniser(prRow(missing)); err != nil {
		t.Fatal(err)
	}

	got, err := d.Recognise("https://github.com/openziti/ziti/pull/4211")
	if err != nil {
		t.Fatal(err)
	}
	if got.CwdExists {
		t.Fatal("a directory that does not exist was reported as being there")
	}
	if !strings.Contains(got.Problem, "not here yet") {
		t.Fatalf("the dialog would say nothing useful: %q", got.Problem)
	}
	if _, err := os.Stat(filepath.FromSlash(missing)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("atrium created the directory. it receives a ready directory and " +
			"makes none, or it has started to learn git")
	}
	// The rest of the resolution still stands. A missing checkout is a thing
	// to go and do, not a reason to throw away the five fields that resolved.
	if got.Title == "" || got.Org != "openziti" {
		t.Fatalf("the resolution was discarded along with the directory: %+v", got)
	}
}

func TestAFetchAddsFactsThePatternCouldNotKnow(t *testing.T) {
	d := testDaemon(t)
	dir := filepath.ToSlash(t.TempDir())
	row := helperFetch(t, prRow(dir),
		`{"title":"dns drops on resume","branch":"fix/dns","reviewers":3,"draft":false,"body":null}`,
		false)
	row.Branch = "{branch}"
	if _, err := d.st.SaveRecogniser(row); err != nil {
		t.Fatal(err)
	}

	got, err := d.Recognise("https://github.com/openziti/ziti/pull/4211")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "openziti/ziti#4211 dns drops on resume" {
		t.Fatalf("the fetched title did not reach the card: %q", got.Title)
	}
	if got.Branch != "fix/dns" {
		t.Fatalf("branch: %q", got.Branch)
	}
	if got.Vars["reviewers"] != "3" || got.Vars["draft"] != "false" {
		t.Fatalf("numbers and booleans did not become text: %v", got.Vars)
	}
	// A null is the tracker saying it has none, which is the same answer as
	// not printing the key. Turning it into the four letters "null" would put
	// them on a card.
	if v, ok := got.Vars["body"]; ok {
		t.Fatalf("a null fact became the string %q", v)
	}
}

// THE CAPTURES WIN. A fetch reads whatever an issue tracker holds, and anybody
// can write into an issue tracker. A fetch that could redefine `repo` could
// move `cwd`, which would mean the contents of an issue chose the directory a
// runner starts in.
func TestAFetchCannotMoveTheDirectory(t *testing.T) {
	d := testDaemon(t)
	real := filepath.ToSlash(t.TempDir())
	row := helperFetch(t, prRow(real+"/{repo}"),
		`{"repo":"../../../elsewhere","org":"attacker","host":"evil.example","url":"https://evil.example"}`,
		false)
	if _, err := d.st.SaveRecogniser(row); err != nil {
		t.Fatal(err)
	}

	got, err := d.Recognise("https://github.com/openziti/ziti/pull/4211")
	if err != nil {
		t.Fatal(err)
	}
	if got.Cwd != real+"/ziti" {
		t.Fatalf("fetched text chose the working directory: %q", got.Cwd)
	}
	if got.Org != "openziti" || got.Host != "github.com" {
		t.Fatalf("fetched text rewrote the card's origin: %+v", got)
	}
	if !strings.Contains(got.Prompt, "https://github.com/openziti/ziti/pull/4211") {
		t.Fatalf("fetched text rewrote the link on the card: %q", got.Prompt)
	}
}

// A fetch is a network call wearing a shell script, so it breaks. What must not
// break with it is the card: the captures already answered most of the dialog.
func TestAFailingFetchStillFillsTheDialogIn(t *testing.T) {
	d := testDaemon(t)
	dir := filepath.ToSlash(t.TempDir())
	row := helperFetch(t, prRow(dir), "", true)
	if _, err := d.st.SaveRecogniser(row); err != nil {
		t.Fatal(err)
	}

	got, err := d.Recognise("https://github.com/openziti/ziti/pull/4211")
	if err != nil {
		t.Fatalf("a broken fetch failed the whole resolution: %v", err)
	}
	if got.Cwd != dir || got.Org != "openziti" {
		t.Fatalf("the captures were thrown away with the fetch: %+v", got)
	}
	if got.FetchError == "" || !strings.Contains(got.FetchError, "not logged in") {
		t.Fatalf("the dialog would not say why the title is thin: %q", got.FetchError)
	}
	// The fetched title never arrived, so the template's hole is still there
	// and named, rather than papered over.
	if !strings.Contains(got.Title, "{title}") {
		t.Fatalf("a missing fact was silently blanked: %q", got.Title)
	}

	saved, err := d.st.RecogniserByID("github-pr")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Failures != 1 || saved.LastError == "" {
		t.Fatalf("the failure is not on the row for anybody to find: %+v", saved)
	}
	if !saved.Enabled {
		t.Fatal("the row switched itself off, so the next paste matches nothing " +
			"while somebody watches the board do nothing")
	}
}

// A path with a placeholder still in it does not exist for an uninteresting
// reason, and "no such directory" about it sends somebody off to create one.
func TestAnUnfilledPlaceholderIsReportedBeforeTheDirectoryIs(t *testing.T) {
	d := testDaemon(t)
	row := prRow(filepath.ToSlash(t.TempDir()) + "/{branch}")
	if _, err := d.st.SaveRecogniser(row); err != nil {
		t.Fatal(err)
	}

	got, err := d.Recognise("https://github.com/openziti/ziti/pull/4211")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Problem, "{branch}") {
		t.Fatalf("the problem does not name the placeholder: %q", got.Problem)
	}
	if strings.Contains(got.Problem, "not here yet") {
		t.Fatalf("it told somebody to go and make a directory with a hole in its name: %q",
			got.Problem)
	}
}

func TestNothingRecognisesAnUnknownURL(t *testing.T) {
	d := testDaemon(t)
	if _, err := d.st.SaveRecogniser(prRow(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Recognise("https://example.invalid/thing/1"); !errors.Is(err, store.ErrNoRecogniser) {
		t.Fatalf("wanted ErrNoRecogniser, got %v", err)
	}
}

// Bounded WHILE reading, the same as a source. A fetch answering one issue
// number with a megabyte is reporting a repository.
func TestAFetchThatPrintsTooMuchIsStopped(t *testing.T) {
	d := testDaemon(t)
	row := helperFetch(t, prRow(filepath.ToSlash(t.TempDir())),
		strings.Repeat("x", recogniserFetchLimit+1024), false)
	if _, err := d.st.SaveRecogniser(row); err != nil {
		t.Fatal(err)
	}
	got, err := d.Recognise("https://github.com/openziti/ziti/pull/4211")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.FetchError, "limit") {
		t.Fatalf("an unbounded fetch was read in full: %q", got.FetchError)
	}
}

func TestAFetchThatPrintsNonsenseSaysSo(t *testing.T) {
	d := testDaemon(t)
	row := helperFetch(t, prRow(filepath.ToSlash(t.TempDir())), "not json at all", false)
	if _, err := d.st.SaveRecogniser(row); err != nil {
		t.Fatal(err)
	}
	got, err := d.Recognise("https://github.com/openziti/ziti/pull/4211")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.FetchError, "json") {
		t.Fatalf("the reason is not readable: %q", got.FetchError)
	}
}

// THE BUILT-IN FORGE FETCH on a room with a hub asks the hub, under gh's fact names, and fetches no head for a paste.
func TestTheForgeFetchAsksTheHub(t *testing.T) {
	d := testDaemon(t)
	var asked []forge.HubAsk
	var paths []string
	d.SetHubForge(forge.NewRemote(func(_ context.Context, path string, in, out any) error {
		if path == forge.HubRecognisersPath {
			return errOlderHub
		}
		paths = append(paths, path)
		asked = append(asked, in.(forge.HubAsk))
		switch o := out.(type) {
		case *forge.HubPR:
			*o = forge.HubPR{Kind: forge.GitHub, PR: &forge.PR{Title: "dns drops on resume", HeadRef: "fix/dns", BaseRef: "main"}}
		case *forge.HubIssue:
			*o = forge.HubIssue{Kind: forge.GitHub, Issue: &forge.Issue{Title: "it broke", Body: "steps"}}
		}
		return nil
	}))
	row := prRow(filepath.ToSlash(t.TempDir()))
	row.Fetch, row.FetchArgs = "forge", []string{"pr"}
	row.Branch = "{headRefName}"
	if _, err := d.st.SaveRecogniser(row); err != nil {
		t.Fatal(err)
	}
	got, err := d.Recognise("https://github.com/openziti/ziti/pull/4211")
	if err != nil {
		t.Fatal(err)
	}
	if got.FetchError != "" || got.Title != "openziti/ziti#4211 dns drops on resume" || got.Branch != "fix/dns" {
		t.Fatalf("card = %+v", got)
	}
	if len(asked) != 1 || paths[0] != forge.HubPRPath || asked[0].Fetch || asked[0].Number != 4211 ||
		asked[0].Org != "openziti" || asked[0].Host != "github.com" {
		t.Fatalf("asked %v %+v", paths, asked)
	}

	issue := store.Recogniser{ID: "github-issue", Label: "issue", Enabled: true, Rank: 11,
		Pattern: `^https?://(?P<host>github\.com)/(?P<org>[^/]+)/(?P<repo>[^/]+)/issues/(?P<num>\d+)`,
		Title:   "{title}", Prompt: "{body}", Fetch: "forge", FetchArgs: []string{"issue"}}
	if _, err := d.st.SaveRecogniser(issue); err != nil {
		t.Fatal(err)
	}
	got, err = d.Recognise("https://github.com/openziti/ziti/issues/9")
	if err != nil {
		t.Fatal(err)
	}
	if got.FetchError != "" || got.Title != "it broke" || paths[len(paths)-1] != forge.HubIssuePath {
		t.Fatalf("card = %+v, paths %v", got, paths)
	}
}

// A ROOM WITH A HUB DOES NOT RUN gh AS A FETCH, and says to use the forge fetch.
func TestARoomWithAHubRefusesAGhFetch(t *testing.T) {
	d := testDaemon(t)
	d.SetHubForge(forge.NewRemote(func(_ context.Context, path string, _, _ any) error {
		if path == forge.HubRecognisersPath {
			return errOlderHub
		}
		t.Fatal("the hub was asked for a gh row")
		return nil
	}))
	row := prRow(filepath.ToSlash(t.TempDir()))
	row.Fetch, row.FetchArgs = "gh", []string{"pr", "view", "{url}", "--json", "title"}
	if _, err := d.st.SaveRecogniser(row); err != nil {
		t.Fatal(err)
	}
	got, err := d.Recognise("https://github.com/openziti/ziti/pull/4211")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.FetchError, "through its hub") || !strings.Contains(got.FetchError, "`forge`") {
		t.Fatalf("FetchError = %q", got.FetchError)
	}
}

func TestTheForgeFetchTakesPrOrIssue(t *testing.T) {
	d := testDaemon(t)
	d.SetHubForge(forge.NewRemote(func(_ context.Context, path string, _, _ any) error {
		if path == forge.HubRecognisersPath {
			return errOlderHub
		}
		return nil
	}))
	row := prRow(filepath.ToSlash(t.TempDir()))
	row.Fetch, row.FetchArgs = "forge", []string{"commits"}
	if _, err := d.st.SaveRecogniser(row); err != nil {
		t.Fatal(err)
	}
	got, err := d.Recognise("https://github.com/openziti/ziti/pull/4211")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.FetchError, "pr or issue") {
		t.Fatalf("FetchError = %q", got.FetchError)
	}
}

// errOlderHub is a hub that does not serve the recogniser rows, so the room's own table answers.
var errOlderHub = errors.New("the hub does not run a forge for its rooms. update the hub")

// A ROOM WITH A HUB RECOGNISES FROM THE HUB'S ROWS, with none of its own (u-pulls-no-recogniser-on-hub), and its own
// table is not asked while the hub answers.
func TestARoomRecognisesFromTheHubsRows(t *testing.T) {
	d := testDaemon(t)
	local := prRow(filepath.ToSlash(t.TempDir()))
	local.ID, local.Title = "local", "local {num}"
	if _, err := d.st.SaveRecogniser(local); err != nil {
		t.Fatal(err)
	}
	hubRow := prRow(filepath.ToSlash(t.TempDir()))
	hubRow.ID, hubRow.Title = "hub", "hub {num}"
	d.SetHubForge(forge.NewRemote(func(_ context.Context, path string, _, out any) error {
		if path != forge.HubRecognisersPath {
			t.Fatalf("asked %s", path)
		}
		raw, _ := json.Marshal(map[string]any{"recognisers": []store.Recogniser{hubRow}})
		return json.Unmarshal(raw, out)
	}))
	got, err := d.Recognise("https://github.com/openziti/ziti-console/pull/967/changes")
	if err != nil {
		t.Fatal(err)
	}
	if got.Recogniser != "hub" || got.Title != "hub 967" || got.Org != "openziti" || got.Repo != "ziti-console" {
		t.Fatalf("resolved = %+v", got)
	}
	if _, err := d.Recognise("https://example.com/x"); !errors.Is(err, store.ErrNoRecogniser) {
		t.Fatalf("a link the hub's rows do not match = %v", err)
	}
}

// A HUB THAT CANNOT BE ASKED leaves the room on its own table, so a room cut off from its hub still recognises.
func TestARoomWhoseHubCannotBeAskedUsesItsOwnRecognisers(t *testing.T) {
	d := testDaemon(t)
	if _, err := d.st.SaveRecogniser(prRow(filepath.ToSlash(t.TempDir()))); err != nil {
		t.Fatal(err)
	}
	d.SetHubForge(forge.NewRemote(func(context.Context, string, any, any) error { return errOlderHub }))
	got, err := d.Recognise("https://github.com/openziti/ziti/pull/4211")
	if err != nil || got.Recogniser != "github-pr" {
		t.Fatalf("resolved = %+v, %v", got, err)
	}
}
