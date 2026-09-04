package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Refusing a share that has not been set up, in atrium's words.
//
// Without this the start is attempted, the underlying library fails, and what
// reaches the board is zrok's or ziti's own message about a thing that was
// never configured. Those are accurate and they answer a different question:
// they say what broke, not what to do next.

func TestSharingZitiWithNoIdentityIsRefusedWithTheNextStep(t *testing.T) {
	d := testDaemon(t)

	err := d.readyToShare(OverlayZiti)
	if err == nil {
		t.Fatal("a ziti share with no identity was allowed to start")
	}
	if !strings.Contains(err.Error(), "enroll") {
		t.Fatalf("the refusal does not say what to do: %v", err)
	}
}

// Configured and missing is the common state after a machine is rebuilt, and
// it is worth telling apart from never configured. Somebody who has enrolled
// before needs to hear "it has gone", not "there is none".
func TestAnIdentityThatHasGoneSaysSoRatherThanSayingThereIsNone(t *testing.T) {
	d := testDaemon(t)
	gone := filepath.ToSlash(filepath.Join(t.TempDir(), "vanished.json"))
	if err := d.saveOverlay("ziti", []byte(`{"identity":"`+gone+`","service":"board"}`)); err != nil {
		t.Fatal(err)
	}

	err := d.readyToShare(OverlayZiti)
	if err == nil {
		t.Fatal("a missing identity was allowed to start")
	}
	if !strings.Contains(err.Error(), "not there") {
		t.Fatalf("a missing identity reads as an unconfigured one: %v", err)
	}
	if !strings.Contains(err.Error(), gone) {
		t.Fatalf("the refusal does not name the file: %v", err)
	}
}

// An identity with no service named would start a listener with nothing to
// bind, which comes back from ziti as a refusal that reads like a permissions
// problem.
func TestAnIdentityWithNoServiceIsRefused(t *testing.T) {
	d := testDaemon(t)
	dir := t.TempDir()
	id := filepath.Join(dir, "id.json")
	if err := os.WriteFile(id, []byte(`{"ztAPI":"https://ctrl.example:1280"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.saveOverlay("ziti", []byte(`{"identity":"`+filepath.ToSlash(id)+`"}`)); err != nil {
		t.Fatal(err)
	}

	err := d.readyToShare(OverlayZiti)
	if err == nil {
		t.Fatal("a ziti share with no service was allowed to start")
	}
	if !strings.Contains(err.Error(), "service") {
		t.Fatalf("the refusal does not name what is missing: %v", err)
	}
}

// A fully configured ziti overlay passes the pre-flight. This is the case that
// would be easy to break while making the refusals work, and breaking it means
// the overlay can never start at all.
func TestAConfiguredZitiOverlayPassesPreflight(t *testing.T) {
	d := testDaemon(t)
	dir := t.TempDir()
	id := filepath.Join(dir, "id.json")
	if err := os.WriteFile(id, []byte(`{"ztAPI":"https://ctrl.example:1280"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.saveOverlay("ziti",
		[]byte(`{"identity":"`+filepath.ToSlash(id)+`","service":"atrium-board"}`)); err != nil {
		t.Fatal(err)
	}

	if err := d.readyToShare(OverlayZiti); err != nil {
		t.Fatalf("a configured overlay was refused: %v", err)
	}
}

// An overlay nobody has heard of is not silently allowed through the
// pre-flight, which would then reach a start that has no case for it.
func TestAnUnknownOverlayCannotBeStarted(t *testing.T) {
	d := testDaemon(t)
	if err := d.startOverlay("carrier-pigeon"); err == nil {
		t.Fatal("an unknown overlay was started")
	}
}

// The pre-flight is a pre-flight: it must not be reachable only through the
// happy path, or a caller that goes straight to the start skips it.
func TestStartGoesThroughThePreflight(t *testing.T) {
	d := testDaemon(t)
	err := d.startOverlay("ziti")
	if err == nil {
		t.Fatal("starting ziti with nothing configured was allowed")
	}
	// The pre-flight's wording rather than the library's, which is the whole
	// point of having one.
	if !strings.Contains(err.Error(), "enroll") {
		t.Fatalf("the start did not go through the pre-flight: %v", err)
	}
}
