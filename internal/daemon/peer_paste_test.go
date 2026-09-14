package daemon

import (
	"strings"
	"testing"
)

// Regression for B2-47: long peer reports must arrive as one paste.
// PTY writes can be split into chunks, which runners may interpret as separate
// input without markers. stubbornPty simulates this by limiting each write.

// longReport is a message past the pipe's four kilobytes, which is the size at
// which this went wrong.
func longReport() string {
	return "wave 1 done. " + strings.Repeat("a sentence that carries some of the report. ", 200)
}

func TestALongPeerMessageIsBracketedAndWhole(t *testing.T) {
	report := longReport()
	if len(report) < 4096 {
		t.Fatalf("the specimen is %d bytes, which is under the pipe size this is about", len(report))
	}
	// 4096 is what a ConPTY write was measured at.
	p := &stubbornPty{most: 4096}
	r := &runner{pty: p}

	if err := r.SayPasted(report); err != nil {
		t.Fatal(err)
	}
	got := string(p.got)

	if p.calls < 2 {
		t.Fatal("the specimen went in one write, so this proves nothing about installments")
	}
	if !strings.HasPrefix(got, "\x1b[200~") {
		t.Fatal("the paste does not open with the bracketed paste marker")
	}
	if !strings.Contains(got, "\x1b[201~") {
		t.Fatal("the paste never closes its bracket, so the runner keeps reading")
	}
	// The close marker comes before the Enter, or the Enter is inside the
	// paste and the runner receives a newline rather than a submission.
	if strings.Index(got, "\x1b[201~") > strings.LastIndex(got, "\r") {
		t.Fatal("the Enter is inside the brackets, so nothing is submitted")
	}
	if !strings.HasSuffix(got, "\r") {
		t.Fatal("nothing submitted the message")
	}
	// Verify every byte survived the short writes.
	if !strings.Contains(got, report) {
		t.Fatal("the report was truncated or altered on the way through")
	}
}

// A runner that never declared bracketed paste gets the plain path, because
// markers it does not understand are printed as `200~` on its screen and into
// its transcript.
func TestAnUndeclaredRunnerIsNotBracketedByAPeer(t *testing.T) {
	p := &stubbornPty{most: 4096}
	r := &runner{pty: p}

	if err := r.Say("a short message"); err != nil {
		t.Fatal(err)
	}
	got := string(p.got)
	if strings.Contains(got, "\x1b[200~") || strings.Contains(got, "\x1b[201~") {
		t.Fatalf("a plain Say sent paste markers: %q", got)
	}
	if !strings.HasSuffix(got, "\r") {
		t.Fatal("nothing submitted the message")
	}
}
