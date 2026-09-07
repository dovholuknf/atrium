package store

import (
	"errors"
	"strings"
	"testing"
)

// A recogniser row that would be typical of this operator's table.
func pullRequestRow() Recogniser {
	return Recogniser{
		ID:      "github-pr",
		Label:   "github pull request",
		Enabled: true,
		Rank:    10,
		Pattern: `^https?://(?P<host>github\.com)/(?P<org>[^/]+)/(?P<repo>[^/]+)/pull/(?P<num>\d+)`,
		Kind:    "github",
		Title:   "{org}/{repo}#{num}",
		Tags:    "pull-request,{repo}",
		Cwd:     "d:/worktrees/{org}/{repo}/pr-{num}",
		Window:  "pull-requests",
		Prompt:  "review the pull request at {url}",
	}
}

func TestSaveRecogniserRefusesAPatternThatDoesNotCompile(t *testing.T) {
	s := open(t)
	r := pullRequestRow()
	r.Pattern = `(?P<org>[^/]+` // no closing paren
	_, err := s.SaveRecogniser(r)
	if err == nil {
		t.Fatal("a pattern that does not compile was saved. it would have failed " +
			"at the moment somebody pasted a url, with nobody around who could fix it")
	}
	if !strings.Contains(err.Error(), "regular expression") {
		t.Fatalf("the refusal does not say what is wrong: %v", err)
	}
}

func TestSaveRecogniserNeedsAPattern(t *testing.T) {
	s := open(t)
	r := pullRequestRow()
	r.Pattern = "   "
	if _, err := s.SaveRecogniser(r); err == nil {
		t.Fatal("a recogniser with no pattern was saved, and it can never match anything")
	}
}

// The order of the table is the dispatch. `.../pull/5/files` and `.../pull/5`
// are the same pull request, and a generic "any repo on a known host" row will
// happily swallow both, so the specific rows have to be asked first.
func TestMostSpecificRecogniserWins(t *testing.T) {
	s := open(t)
	if _, err := s.SaveRecogniser(pullRequestRow()); err != nil {
		t.Fatal(err)
	}
	generic := Recogniser{
		ID: "github-repo", Label: "a repository", Enabled: true, Rank: 900,
		Pattern: `^https?://(?P<host>github\.com)/(?P<org>[^/]+)/(?P<repo>[^/]+)`,
		Cwd:     "d:/git/{org}/{repo}",
	}
	if _, err := s.SaveRecogniser(generic); err != nil {
		t.Fatal(err)
	}

	got, vars, err := s.MatchRecogniser("https://github.com/openziti/ziti/pull/4211/files")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "github-pr" {
		t.Fatalf("the generic row swallowed a pull request: matched %s", got.ID)
	}
	if vars["num"] != "4211" {
		t.Fatalf("the pull request number was not captured: %q", vars["num"])
	}

	// And the generic one still catches what the specific one does not, which
	// is why it is at the bottom rather than deleted.
	got, _, err = s.MatchRecogniser("https://github.com/openziti/ziti")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "github-repo" {
		t.Fatalf("a bare repo url matched %s", got.ID)
	}
}

func TestDisabledRecognisersAreNotAsked(t *testing.T) {
	s := open(t)
	r := pullRequestRow()
	r.Enabled = false
	if _, err := s.SaveRecogniser(r); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.MatchRecogniser("https://github.com/openziti/ziti/pull/1"); !errors.Is(err, ErrNoRecogniser) {
		t.Fatalf("a switched-off recogniser answered: %v", err)
	}
}

func TestUnmatchedURLIsNotAnError(t *testing.T) {
	s := open(t)
	if _, err := s.SaveRecogniser(pullRequestRow()); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.MatchRecogniser("https://example.invalid/whatever")
	if !errors.Is(err, ErrNoRecogniser) {
		t.Fatalf("wanted ErrNoRecogniser so the caller can say 'nothing knows what that is', got %v", err)
	}
}

// Atrium must not decide what a URL means. A pattern author who wants to accept
// a bare host writes the scheme as optional and has said so; one who did not
// gets no help, which is the behaviour they can reason about.
func TestNoURLNormalisationHappens(t *testing.T) {
	s := open(t)
	if _, err := s.SaveRecogniser(pullRequestRow()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.MatchRecogniser("github.com/openziti/ziti/pull/4211"); !errors.Is(err, ErrNoRecogniser) {
		t.Fatal("a scheme was invented for a url that did not have one")
	}
	// Whitespace is the one thing trimmed, because a pasted url arrives with a
	// newline on it and no pattern should have to know that.
	if _, _, err := s.MatchRecogniser("  https://github.com/openziti/ziti/pull/4211\n"); err != nil {
		t.Fatalf("a pasted url with whitespace round it was refused: %v", err)
	}
}

func TestFillSubstitutesCapturesIntoEveryTemplate(t *testing.T) {
	s := open(t)
	if _, err := s.SaveRecogniser(pullRequestRow()); err != nil {
		t.Fatal(err)
	}
	r, vars, err := s.MatchRecogniser("https://github.com/openziti/ziti/pull/4211")
	if err != nil {
		t.Fatal(err)
	}
	got := r.Fill(vars)

	if got.Title != "openziti/ziti#4211" {
		t.Fatalf("title: %q", got.Title)
	}
	if got.Cwd != "d:/worktrees/openziti/ziti/pr-4211" {
		t.Fatalf("cwd: %q", got.Cwd)
	}
	if got.Repo != "ziti" || got.Org != "openziti" || got.Host != "github.com" {
		t.Fatalf("the card's own fields did not come off the captures: %+v", got)
	}
	if got.Window != "pull-requests" {
		t.Fatalf("window: %q", got.Window)
	}
	if !strings.Contains(got.Prompt, "https://github.com/openziti/ziti/pull/4211") {
		t.Fatalf("the prompt does not carry the url: %q", got.Prompt)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "pull-request" || got.Tags[1] != "ziti" {
		t.Fatalf("tags: %v", got.Tags)
	}
	if len(got.Missing) != 0 {
		t.Fatalf("nothing should be missing here: %v", got.Missing)
	}
}

// The whole reason this is not "blank it and carry on": `feature/{branch}` with
// nothing to put in it becomes `feature/`, which is a directory somebody
// creates by accident and then wonders about.
func TestAPlaceholderNothingFilledIsLeftStandingAndReported(t *testing.T) {
	s := open(t)
	r := pullRequestRow()
	r.Cwd = "d:/worktrees/{org}/{repo}/{branch}"
	if _, err := s.SaveRecogniser(r); err != nil {
		t.Fatal(err)
	}
	row, vars, err := s.MatchRecogniser("https://github.com/openziti/ziti/pull/4211")
	if err != nil {
		t.Fatal(err)
	}
	got := row.Fill(vars)
	if !strings.Contains(got.Cwd, "{branch}") {
		t.Fatalf("the hole was papered over rather than shown: %q", got.Cwd)
	}
	if len(got.Missing) != 1 || got.Missing[0] != "branch" {
		t.Fatalf("wanted branch reported as missing, got %v", got.Missing)
	}
}

// A title fetched from an issue tracker is somebody else's text, and somebody
// else's text has newlines in it. A card title is one line and a path is a path.
func TestFetchedTextDoesNotBreakOutOfItsField(t *testing.T) {
	r := &Recogniser{
		ID: "x", Title: "{title}", Cwd: "d:/w/{repo}", Prompt: "fix {title}",
	}
	got := r.Fill(map[string]string{
		"title": "crash on start\r\nrm -rf everything",
		"repo":  "ziti\nelsewhere",
	})
	if strings.ContainsAny(got.Title, "\r\n") {
		t.Fatalf("a two-line title reached the card: %q", got.Title)
	}
	if strings.ContainsAny(got.Cwd, "\r\n") {
		t.Fatalf("a newline reached a path: %q", got.Cwd)
	}
	// The prompt is the exception on purpose: an issue body is paragraphs, and
	// flattening it would be atrium mangling somebody else's words in transit.
	if !strings.Contains(got.Prompt, "\n") {
		t.Fatalf("the prompt was flattened, so an issue body would arrive as one line: %q", got.Prompt)
	}
}

// A capture group called `url` does not get to redefine what {url} means, or
// every prompt built on it would point somewhere the operator did not paste.
func TestURLAlwaysMeansTheURL(t *testing.T) {
	s := open(t)
	r := pullRequestRow()
	r.Pattern = `^(?P<url>https?://)(?P<host>github\.com)/(?P<org>[^/]+)/(?P<repo>[^/]+)/pull/(?P<num>\d+)`
	r.Prompt = "look at {url}"
	if _, err := s.SaveRecogniser(r); err != nil {
		t.Fatal(err)
	}
	row, vars, err := s.MatchRecogniser("https://github.com/openziti/ziti/pull/7")
	if err != nil {
		t.Fatal(err)
	}
	got := row.Fill(vars)
	if got.Prompt != "look at https://github.com/openziti/ziti/pull/7" {
		t.Fatalf("a capture redefined the url: %q", got.Prompt)
	}
}

func TestFillArgvKeepsEveryArgumentSeparate(t *testing.T) {
	cmd, args := FillArgv("gh", []string{"issue", "view", "{num}", "--repo", "{org}/{repo}"},
		map[string]string{"num": "12", "org": "open ziti", "repo": "ziti"})
	if cmd != "gh" {
		t.Fatalf("cmd: %q", cmd)
	}
	if len(args) != 5 {
		t.Fatalf("arguments were joined or split: %q", args)
	}
	if args[4] != "open ziti/ziti" {
		t.Fatalf("a value with a space in it stopped being one argument: %q", args[4])
	}
}

// The failure bookkeeping is what atrium observed. It is not writable, the same
// as a source, and turning a row back on is the operator saying they fixed it.
func TestRecogniserFailuresAreObservedNotTyped(t *testing.T) {
	s := open(t)
	r := pullRequestRow()
	if _, err := s.SaveRecogniser(r); err != nil {
		t.Fatal(err)
	}
	if err := s.RecogniserFetched(r.ID, errors.New("gh: not logged in\nrun gh auth login")); err != nil {
		t.Fatal(err)
	}
	got, err := s.RecogniserByID(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Failures != 1 || got.LastError != "gh: not logged in" {
		t.Fatalf("the failure was not recorded on the row: %+v", got)
	}
	if got.LastUsedAt == nil {
		t.Fatal("a row that was used has no last-used time")
	}

	// A save with a made-up count does not get to write it.
	r.Failures = 99
	r.LastError = "everything is fine"
	saved, err := s.SaveRecogniser(r)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Failures != 1 || saved.LastError != "gh: not logged in" {
		t.Fatalf("observed state was overwritten by an edit: %+v", saved)
	}
}

// UNLIKE A SOURCE. A source is a timer nobody is watching, so switching a
// broken one off is a kindness. A recogniser is a verb somebody just typed with
// the board in front of them, and a row that switched itself off would make the
// next paste silently match nothing while they watched.
func TestAFailingFetchNeverSwitchesTheRowOff(t *testing.T) {
	s := open(t)
	if _, err := s.SaveRecogniser(pullRequestRow()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := s.RecogniserFetched("github-pr", errors.New("gh exploded")); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.RecogniserByID("github-pr")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled {
		t.Fatal("five failed fetches switched the row off. the next url pasted would " +
			"match nothing, and 'gh was not logged in' would read as 'atrium is broken'")
	}
	if _, _, err := s.MatchRecogniser("https://github.com/openziti/ziti/pull/1"); err != nil {
		t.Fatalf("the row stopped matching after its fetch failed: %v", err)
	}
}

func TestDeleteRecogniser(t *testing.T) {
	s := open(t)
	if _, err := s.SaveRecogniser(pullRequestRow()); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRecogniser("github-pr"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Recognisers()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("still there: %v", rows)
	}
}

// A row hand-edited into the database, or imported from a file written by
// hand, can carry a pattern that does not compile. One of those must not stop
// the rows below it from being asked.
func TestABadPatternInTheTableDoesNotBlockTheRestOfIt(t *testing.T) {
	s := open(t)
	if _, err := s.SaveRecogniser(pullRequestRow()); err != nil {
		t.Fatal(err)
	}
	if err := s.guard(func() error {
		_, err := s.db.Exec(`INSERT INTO recogniser (id, label, enabled, rank, pattern, created_at)
			VALUES ('broken','broken',1,1,'(?P<org>[^/]+', ?)`, ts(now()))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.MatchRecogniser("https://github.com/openziti/ziti/pull/4211")
	if err != nil {
		t.Fatalf("one uncompilable row stopped the whole table: %v", err)
	}
	if got.ID != "github-pr" {
		t.Fatalf("matched %s", got.ID)
	}
}
