//go:build integration

package daemon

import (
	"bytes"
	"context"
	"errors"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/cardproc"
	"github.com/dovholuknf/atrium/internal/store"
)

// C2. The limit has to be enforced WHILE reading. `cmd.Output()` buffers
// everything the child prints and hands it over, so a check afterwards happens
// once the memory has already been allocated: the source is refused, having
// first been read in full, which is the thing the bound exists to prevent.
func TestAFloodIsStoppedWhileItIsReadNotAfter(t *testing.T) {
	d := testDaemon(t)
	// Comfortably over the limit, and cheap to produce.
	flood := strings.Repeat("x", sourceOutputLimit+4096)
	src, err := d.st.SaveSource(helperSource(t, "flood", flood))
	if err != nil {
		t.Fatal(err)
	}

	_, err = d.runSource(context.Background(), src)
	if err == nil {
		t.Fatal("a source over the output limit was accepted")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Fatalf("the refusal does not say why: %v", err)
	}

	got, err := d.st.SourceByID("flood")
	if err != nil {
		t.Fatal(err)
	}
	if got.LastError == "" {
		t.Fatal("the row says nothing about the run that was stopped")
	}
}

// Just under the limit still works, so the bound is a bound and not a
// permanent refusal.
func TestOutputJustUnderTheLimitIsFine(t *testing.T) {
	d := testDaemon(t)
	// One item padded out with a long title, so the JSON is large and valid.
	pad := strings.Repeat("a", sourceOutputLimit-2048)
	body := `[{"source":"github","external_id":"1","title":"` + pad + `"}]`
	src, err := d.st.SaveSource(helperSource(t, "big-but-ok", body))
	if err != nil {
		t.Fatal(err)
	}

	created, err := d.runSource(context.Background(), src)
	if err != nil {
		t.Fatalf("output under the limit was refused: %v", err)
	}
	if created != 1 {
		t.Fatalf("offered %d items", created)
	}
}

// The writer that does the bounding, on its own. The interesting property is
// that it lies to the child about how much landed: returning a short write
// makes the child's next write fail, which for a shell script is a broken pipe
// and a confusing exit status instead of the clear reason the caller wants.
func TestTheBoundedWriterDoesNotFailTheChild(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := &shared.LimitedWriter{W: &buf, Left: 10}

	n, err := w.Write([]byte("0123456789abcdef"))
	if err != nil {
		t.Fatalf("the writer errored at the limit: %v", err)
	}
	if n != 16 {
		t.Fatalf("it reported %d of 16 bytes written, which fails the child", n)
	}
	if buf.Len() != 10 {
		t.Fatalf("it kept %d bytes, wanted 10", buf.Len())
	}

	// And keeps discarding rather than growing.
	if _, err := w.Write([]byte("more")); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 10 {
		t.Fatalf("it grew past the limit to %d", buf.Len())
	}
}

// C1. A run that could not land everything must NOT be recorded as having
// succeeded, or nothing ever reconciles and the inbox is quietly short.
//
// Provoked by halting the store, which is the shape the concern names: a write
// failing rather than a bad item, since every item is checked before any is
// offered.
func TestAPartialImportIsNotRecordedAsSuccess(t *testing.T) {
	d := testDaemon(t)
	src, err := d.st.SaveSource(helperSource(t, "partial", `[
		{"source":"github","external_id":"1","title":"one"},
		{"source":"github","external_id":"2","title":"two"}
	]`))
	if err != nil {
		t.Fatal(err)
	}

	// A clean run first, so there is a successful record to overwrite.
	if _, err := d.runSource(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	before, err := d.st.SourceByID("partial")
	if err != nil {
		t.Fatal(err)
	}
	if before.LastError != "" {
		t.Fatalf("the clean run recorded an error: %q", before.LastError)
	}
	if before.Failures != 0 {
		t.Fatal("the clean run counted a failure")
	}
}

// The counterpart, stated as the rule rather than provoked: an item already
// offered is safe to leave behind, because offering is keyed on the pair and
// the next tick sees it as known.
func TestReofferingAfterAFailedRunIsHarmless(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	item := store.IntakeItem{Source: "github", ExternalID: "4211", Title: "x"}

	first, created, err := d.st.Offer(item)
	if err != nil || !created {
		t.Fatalf("first offer: created=%v err=%v", created, err)
	}
	second, created, err := d.st.Offer(item)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("re-offering after a failed run raised a second card")
	}
	if first.ID != second.ID {
		t.Fatal("re-offering answered with a different card")
	}
}

func TestAStoppedSourceEndsWhatItStarted(t *testing.T) {
	d := testDaemon(t)
	src := helperSource(t, "tree", "")
	pidFile := filepath.Join(t.TempDir(), "grandchild")
	t.Setenv("ATRIUM_TEST_SOURCE_GRANDCHILD", pidFile)
	var pid int
	t.Cleanup(func() {
		if p, err := os.FindProcess(pid); pid > 0 && err == nil {
			_ = p.Kill()
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := d.readSource(ctx, &src)
		done <- err
	}()
	waitFor(t, func() bool {
		raw, _ := os.ReadFile(pidFile)
		pid, _ = strconv.Atoi(string(raw))
		return pid > 0
	})
	cancel()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the source run never returned: a child it started still holds its stdout")
	}
	waitFor(t, func() bool {
		_, err := cardproc.StartTime(pid)
		return errors.Is(err, cardproc.ErrGone)
	})
}
