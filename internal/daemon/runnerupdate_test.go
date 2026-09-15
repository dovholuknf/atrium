package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// The comparison the whole feature rests on. String comparison says 0.9.0 is
// newer than 0.10.0, which would raise a card telling somebody to downgrade.
func TestNewerVersionIsNumericPerSegment(t *testing.T) {
	cases := []struct {
		latest, installed string
		want              bool
	}{
		{"0.10.0", "0.9.0", true},
		{"0.9.0", "0.10.0", false},
		{"2.1.270", "2.1.270", false},
		{"2.1.271", "2.1.270", true},
		{"2.2.0", "2.1.999", true},
		{"1.2.3", "1.2", true},
		{"1.2", "1.2.3", false},
		// A prerelease compares as the release it is a candidate for, which is
		// the documented limit rather than a bug.
		{"1.3.0", "1.3.0-rc1", false},
		// No opinion rather than a guess. Anything unparseable reports nothing.
		{"", "1.0.0", false},
		{"1.0.0", "", false},
		{"nightly", "1.0.0", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.latest, c.installed); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v",
				c.latest, c.installed, got, c.want)
		}
	}
}

// The installed version comes off disk and NOT by running the runner, which is
// the point of the whole file. Both npm layouts are covered because Windows
// uses one and every version manager uses the other.
func TestInstalledVersionIsReadFromTheInstalledPackage(t *testing.T) {
	const pkg = "@anthropic-ai/claude-code"

	t.Run("beside the launcher", func(t *testing.T) {
		root := t.TempDir()
		writePackageJSON(t, filepath.Join(root, "node_modules", pkg), "2.1.270")
		exe := filepath.Join(root, "claude.cmd")
		if err := os.WriteFile(exe, []byte("@echo off"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := installedVersion(exe, pkg); got != "2.1.270" {
			t.Fatalf("got %q, want 2.1.270", got)
		}
	})

	t.Run("under a unix prefix", func(t *testing.T) {
		root := t.TempDir()
		writePackageJSON(t, filepath.Join(root, "lib", "node_modules", pkg), "0.154.0")
		exe := filepath.Join(root, "bin", "claude")
		if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0o700); err != nil {
			t.Fatal(err)
		}
		if got := installedVersion(exe, pkg); got != "0.154.0" {
			t.Fatalf("got %q, want 0.154.0", got)
		}
	})

	t.Run("nothing to read is an empty answer, not a guess", func(t *testing.T) {
		root := t.TempDir()
		exe := filepath.Join(root, "claude")
		if err := os.WriteFile(exe, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := installedVersion(exe, pkg); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
		if got := installedVersion("", pkg); got != "" {
			t.Fatalf("no path got %q, want empty", got)
		}
		if got := installedVersion(exe, ""); got != "" {
			t.Fatalf("no package got %q, want empty", got)
		}
	})
}

// A scoped package name has a slash in it, and a slash in a URL path is a
// different resource. Getting this wrong asks the registry about `@openai`.
func TestLatestPublishedEscapesAScopedName(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.EscapedPath()
		json.NewEncoder(w).Encode(map[string]string{"version": "0.155.0"})
	}))
	defer srv.Close()

	old := updateRegistry
	updateRegistry = srv.URL
	defer func() { updateRegistry = old }()

	got, err := latestPublished(context.Background(), "@openai/codex")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.155.0" {
		t.Fatalf("got %q, want 0.155.0", got)
	}
	if !strings.Contains(asked, "%2f") && !strings.Contains(asked, "%2F") {
		t.Fatalf("the scope separator went through unescaped: %s", asked)
	}
}

// ONLY THE FIRST CALLER GOES AND ASKS. A wave of launches must not become a
// wave of requests, and the ones that lose must not queue behind the one that
// won.
func TestOnlyOneCheckIsInFlightPerPackage(t *testing.T) {
	var u updates
	mine, ch := u.claim("@openai/codex")
	if !mine {
		t.Fatal("the first caller was not given the claim")
	}
	again, same := u.claim("@openai/codex")
	if again {
		t.Fatal("a second caller was also told to go and ask")
	}
	if same != ch {
		t.Fatal("the second caller was given a different channel to wait on")
	}
	// A different package is a different question and is not blocked by this
	// one.
	if other, _ := u.claim("@anthropic-ai/claude-code"); !other {
		t.Fatal("a second package was held up behind the first")
	}
	u.release("@openai/codex", ch)
	select {
	case <-ch:
	default:
		t.Fatal("releasing did not wake what was waiting")
	}
	if back, _ := u.claim("@openai/codex"); !back {
		t.Fatal("the claim was not released")
	}
}

// Concurrent claims must hand exactly one caller the work.
func TestClaimIsRacefree(t *testing.T) {
	var u updates
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if mine, _ := u.claim("p"); mine {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("%d callers were told to go and ask, want 1", winners)
	}
}

// The bug the operator saw: two identical rows in the inbox. The key names the
// WORK and not the state of it, so a second release rewrites the card that is
// already there.
func TestASecondReleaseRewritesTheCardRatherThanAddingOne(t *testing.T) {
	d := testDaemon(t)
	item := store.IntakeItem{
		Source:     "runner-update",
		ExternalID: "@anthropic-ai/claude-code",
		Title:      "claude code: 2.1.270 to 2.1.271",
		Why:        "a newer claude code is published.",
	}
	first, created, err := d.st.Offer(item)
	if err != nil || !created {
		t.Fatalf("first offer: created=%v err=%v", created, err)
	}

	item.Title = "claude code: 2.1.270 to 2.1.272"
	second, created, err := d.st.Offer(item)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("a second release raised a second card")
	}
	if first.ID != second.ID {
		t.Fatal("the second release answered with a different card")
	}
	if second.Title != "claude code: 2.1.270 to 2.1.272" {
		t.Fatalf("the card still says %q", second.Title)
	}

	got, err := d.st.Offered()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, o := range got {
		if o.Source == "runner-update" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the inbox holds %d runner-update cards, want 1", n)
	}
}

// The other half of that rule. Once a card has been started it has a session
// and a history, and a later offer must not rewrite what somebody is working
// in.
func TestAStartedCardIsNotRewrittenByALaterOffer(t *testing.T) {
	d := testDaemon(t)
	item := store.IntakeItem{
		Source: "github", ExternalID: "4211", Title: "the original title",
	}
	first, _, err := d.st.Offer(item)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(first.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}

	item.Title = "a title the source made up later"
	again, created, err := d.st.Offer(item)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("offering a started card raised a second one")
	}
	if again.Title != "the original title" {
		t.Fatalf("a started card was retitled to %q", again.Title)
	}
}

// An empty field in a later offer leaves what is there rather than blanking
// it. Editing the prompt before pressing start is a documented use of the
// inbox, and a source that stops sending one must not wipe that.
func TestARefreshDoesNotBlankWhatItDoesNotSend(t *testing.T) {
	d := testDaemon(t)
	first, _, err := d.st.Offer(store.IntakeItem{
		Source: "github", ExternalID: "9", Title: "t", Prompt: "do the thing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetPrompt(first.ID, "do it carefully"); err != nil {
		t.Fatal(err)
	}
	again, _, err := d.st.Offer(store.IntakeItem{
		Source: "github", ExternalID: "9", Title: "t2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.Prompt != "do it carefully" {
		t.Fatalf("the edited prompt became %q", again.Prompt)
	}
	if again.Title != "t2" {
		t.Fatalf("the title did not move: %q", again.Title)
	}
}

func writePackageJSON(t *testing.T, dir, version string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"name":"x","version":"` + version + `"}`)
	if err := os.WriteFile(filepath.Join(dir, "package.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	_ = runtime.GOOS
}
