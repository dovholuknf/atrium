//go:build integration

package calllog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readLines(t *testing.T, p string) []Line {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var out []Line
	for _, s := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var l Line
		if err := json.Unmarshal([]byte(s), &l); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	return out
}

func TestStartLineIsOnDiskBeforeTheCallEnds(t *testing.T) {
	dir := t.TempDir()
	l := New(dir, "c.log", 0)
	t.Cleanup(l.Close)
	end := l.Begin("mcp", "atrium_say", "card-1", "sg4")
	got := readLines(t, filepath.Join(dir, "c.log"))
	if len(got) != 1 || got[0].Event != "start" || got[0].Name != "atrium_say" || got[0].Caller != "card-1" ||
		got[0].Room != "sg4" || got[0].Start.IsZero() {
		t.Fatalf("start line = %+v", got)
	}
	time.Sleep(5 * time.Millisecond) // wait: a duration needs time to pass
	end(OK, nil)
	got = readLines(t, filepath.Join(dir, "c.log"))
	if len(got) != 2 || got[1].Event != "end" || got[1].ID != got[0].ID || got[1].Outcome != OK ||
		got[1].Ms < 5 || got[1].End == nil || !got[1].Start.Equal(got[0].Start) {
		t.Fatalf("end line = %+v", got)
	}
}

func TestEndLineCarriesTheError(t *testing.T) {
	dir := t.TempDir()
	l := New(dir, "c.log", 0)
	t.Cleanup(l.Close)
	l.Begin("hook", "POST /gate", "", "")(Error, errors.New("boom"))
	got := readLines(t, filepath.Join(dir, "c.log"))
	if got[1].Outcome != Error || got[1].Error != "boom" {
		t.Fatalf("end line = %+v", got[1])
	}
}

func TestOutcomeNamesTimeoutAndClientGone(t *testing.T) {
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	late, cancel2 := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel2()
	<-late.Done()
	if Outcome(context.Background(), nil) != OK || Outcome(gone, errors.New("x")) != ClientGone ||
		Outcome(late, errors.New("x")) != Timeout || Outcome(context.Background(), errors.New("x")) != Error {
		t.Fatal("wrong outcome")
	}
}

func TestLogRotatesBySizeAndKeepsOneOldFile(t *testing.T) {
	dir := t.TempDir()
	l := New(dir, "c.log", 400)
	t.Cleanup(l.Close)
	for i := 0; i < 40; i++ {
		l.Begin("mcp", "t", "c", "r")(OK, nil)
	}
	live, err := os.Stat(filepath.Join(dir, "c.log"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "c.log.1")); err != nil {
		t.Fatal("no rotated file:", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "c.log.2")); err == nil {
		t.Fatal("more than one old file kept")
	}
	if live.Size() > 400+400 {
		t.Fatalf("live file is %d bytes", live.Size())
	}
}

func TestNilAndUnwritableLogNeverFailACall(t *testing.T) {
	var nilLog *Log
	nilLog.Begin("mcp", "t", "", "")(OK, nil)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	New(filepath.Join(blocker, "sub"), "c.log", 0).Begin("mcp", "t", "", "")(OK, nil)
}
