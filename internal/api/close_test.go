package api

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/store"
)

// openForClose opens o/r#7 and answers the card, the worktree and the review row.
func openForClose(t *testing.T) (*openHarness, string, string, *store.PRReview) {
	t.Helper()
	oh := newOpenHarness(t, &fakeForge{pr: forge.PR{HeadRef: "feat/x"}})
	oh.checkout(t, "o", "r")
	code, out := oh.open(t, openURL)
	if code != 201 {
		t.Fatalf("%d %v", code, out)
	}
	row, err := oh.srv.st.PRByID(out["pr"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return oh, out["card"].(string), out["worktree"].(string), row
}

type closeOut struct {
	Code    string        `json:"code"`
	Closed  bool          `json:"closed"`
	Left    int           `json:"left"`
	Asks    []closeAsk    `json:"asks"`
	Items   []closeItem   `json:"items"`
	Stashes []closeStash  `json:"stashes"`
	Preview *closePreview `json:"preview"`
	Warn    []string      `json:"warnings"`
}

func closeCall(t *testing.T, oh *openHarness, card string, body map[string]any) (int, closeOut) {
	t.Helper()
	rec := post(t, oh.h, "/v1/tasks/"+card+"/close", body)
	var out closeOut
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func worktreeSeq(t *testing.T, st *store.Store, card string) int {
	t.Helper()
	rows, _ := st.Resources(card)
	for _, r := range rows {
		if r.Kind == store.ResWorktree {
			return r.Seq
		}
	}
	t.Fatal("no worktree row")
	return 0
}

func exists(p string) bool { _, err := os.Stat(filepath.FromSlash(p)); return err == nil }

func TestCloseFreesWhatTheOpenMadeAndKeepsTheHistory(t *testing.T) {
	oh, card, wt, row := openForClose(t)
	if err := os.MkdirAll(filepath.Join(filepath.FromSlash(row.RunDir), "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(filepath.FromSlash(row.RunDir), "findings"), 0o755); err != nil {
		t.Fatal(err)
	}

	// The preview asks nothing of a clean worktree at the PR head, and changes nothing.
	rec := httpDo(t, oh.h, "GET", "/v1/tasks/"+card+"/close")
	var pv closePreview
	_ = json.Unmarshal(rec.Body.Bytes(), &pv)
	if rec.Code != 200 || len(pv.Asks) != 0 || len(pv.Items) != 4 || !exists(wt) {
		t.Fatalf("preview %d %s", rec.Code, rec.Body.String())
	}

	code, out := closeCall(t, oh, card, map[string]any{"confirm": true})
	if code != 200 || !out.Closed || out.Left != 0 {
		t.Fatalf("%d %+v", code, out)
	}
	if exists(wt) {
		t.Error("the worktree is still on disk")
	}
	got, _ := oh.srv.st.PRByID(row.ID)
	if got.Archived == "" || !strings.Contains(got.RunDir, "-closed-") {
		t.Errorf("the row is not archived and set aside: %+v", got)
	}
	if !exists(filepath.Join(got.RunDir, "findings")) || exists(filepath.Join(got.RunDir, "src")) {
		t.Error("the run folder should keep its findings and lose src")
	}
	if len(oh.killed) != 1 || oh.killed[0] != card {
		t.Errorf("the session was not stopped: %v", oh.killed)
	}
	if task, _ := oh.srv.st.Get(card); task.Status != store.StatusDone {
		t.Errorf("the card is %s", task.Status)
	}
	rows, _ := oh.srv.st.Resources(card)
	for _, r := range rows {
		if r.Live() {
			t.Errorf("%s %s is still live: %s", r.Kind, r.Ref, r.FreedErr)
		}
	}

	// A re-paste is a fresh review, not the closed one.
	code2, again := oh.open(t, openURL)
	if code2 != 201 || again["created"] != true || again["pr"] == row.ID {
		t.Errorf("the reopen: %d %v", code2, again)
	}
}

func TestCloseAsksAboutWorkThatIsNowhereElseAndKeepsWhatItIsTold(t *testing.T) {
	oh, card, wt, _ := openForClose(t)
	if err := os.WriteFile(filepath.Join(filepath.FromSlash(wt), "notes.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := closeCall(t, oh, card, map[string]any{"confirm": true})
	if code != 409 || out.Code != "needs_answer" || out.Preview == nil || len(out.Preview.Asks) != 1 ||
		out.Preview.Asks[0].Dirty != 1 || out.Preview.Asks[0].Branch != "feat/x" {
		t.Fatalf("%d %+v", code, out)
	}
	if !exists(wt) || len(oh.killed) != 0 {
		t.Fatal("an unanswered close changed something")
	}
	seq := strconv.Itoa(worktreeSeq(t, oh.srv.st, card))
	if code, _ := closeCall(t, oh, card, map[string]any{"confirm": true, "answers": map[string]string{seq: "maybe"}}); code != 400 {
		t.Errorf("a wrong answer is %d", code)
	}
	code, out = closeCall(t, oh, card, map[string]any{"confirm": true, "answers": map[string]string{seq: "keep"}})
	if code != 200 || !out.Closed || out.Left != 2 {
		t.Fatalf("%d %+v", code, out)
	}
	if !exists(filepath.Join(filepath.FromSlash(wt), "notes.txt")) {
		t.Error("a kept worktree was removed")
	}
	if got := git(t, wt, "symbolic-ref", "--short", "HEAD"); got != "feat/x" {
		t.Errorf("a kept worktree lost its branch: %q", got)
	}
}

func TestCloseDeletesWhenToldTo(t *testing.T) {
	oh, card, wt, _ := openForClose(t)
	git(t, wt, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "local")
	seq := strconv.Itoa(worktreeSeq(t, oh.srv.st, card))
	rec := httpDo(t, oh.h, "GET", "/v1/tasks/"+card+"/close")
	var pv closePreview
	_ = json.Unmarshal(rec.Body.Bytes(), &pv)
	if len(pv.Asks) != 1 || pv.Asks[0].Unpushed != 1 || pv.Asks[0].Dirty != 0 {
		t.Fatalf("an unpushed commit is not asked about: %s", rec.Body.String())
	}
	code, out := closeCall(t, oh, card, map[string]any{"confirm": true, "answers": map[string]string{seq: "delete"}})
	if code != 200 || out.Left != 0 || exists(wt) {
		t.Fatalf("%d %+v", code, out)
	}
}

func TestCloseStashesToTheHubAndKeepsTheWorktreeWhenTheStashFails(t *testing.T) {
	oh, card, wt, _ := openForClose(t)
	if err := os.WriteFile(filepath.Join(filepath.FromSlash(wt), "notes.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	seq := strconv.Itoa(worktreeSeq(t, oh.srv.st, card))
	if code, out := closeCall(t, oh, card, map[string]any{"confirm": true, "answers": map[string]string{seq: "stash"}}); code != 400 {
		t.Fatalf("a room with no stash took a stash: %d %+v", code, out)
	}

	oh.srv.Stash = func(context.Context, string, string, string) (string, string, error) {
		return "", "", errors.New("the hub is away")
	}
	code, out := closeCall(t, oh, card, map[string]any{"confirm": true, "answers": map[string]string{seq: "stash"}})
	if code != 200 || out.Left != 2 || !exists(wt) || len(out.Warn) == 0 {
		t.Fatalf("a failed stash: %d %+v", code, out)
	}

	var asked []string
	oh.srv.Stash = func(_ context.Context, c, dir, branch string) (string, string, error) {
		asked = append(asked, c, dir, branch)
		return "stash/" + c[:4] + "/" + branch, "github/o/r", nil
	}
	code, out = closeCall(t, oh, card, map[string]any{"confirm": true, "answers": map[string]string{seq: "stash"}})
	if code != 200 || exists(wt) || len(out.Stashes) != 1 || strings.Join(asked, ",") != card+","+wt+",feat/x" {
		t.Fatalf("%d %+v %v", code, out, asked)
	}
	// The stash stays on the card, and is all that does.
	if out.Left != 1 {
		t.Errorf("left %d: %+v", out.Left, out.Items)
	}
}

// A card that walks a review its inventory does not name, one from before cards kept an inventory, archives it too.
func TestCloseArchivesAReviewTheCardWalksOutsideItsInventory(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{pr: forge.PR{HeadRef: "feat/x"}})
	rec := post(t, oh.h, "/v1/prs", map[string]any{"url": openURL})
	var made struct {
		PR store.PRReview `json:"pr"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &made)
	if made.PR.ID == "" {
		t.Fatalf("no row: %s", rec.Body.String())
	}
	card := cardIn(t, oh.srv.st, t.TempDir())
	if _, err := oh.srv.st.SetPRWalker(made.PR.ID, card.ID); err != nil {
		t.Fatal(err)
	}
	code, out := closeCall(t, oh, card.ID, map[string]any{"confirm": true})
	if code != 200 || !out.Closed {
		t.Fatalf("%d %+v", code, out)
	}
	if got, _ := oh.srv.st.PRByID(made.PR.ID); got.Archived == "" {
		t.Error("the walked review was not archived")
	}
}
