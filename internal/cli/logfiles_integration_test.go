//go:build integration

package cli

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedirectLogsWritesTheLogFilesWhenStderrIsAPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	oldOut, oldErr, oldLog := os.Stdout, os.Stderr, log.Writer()
	os.Stderr = w
	defer func() {
		os.Stdout.Close()
		os.Stderr.Close()
		os.Stdout, os.Stderr = oldOut, oldErr
		log.SetOutput(oldLog)
		setStderrHandle(oldErr)
		w.Close()
	}()

	dir := filepath.Join(t.TempDir(), "logs")
	if err := redirectLogs(dir, "room", false); err != nil {
		t.Fatal(err)
	}
	log.Print("from the log package")
	os.Stdout.WriteString("to stdout")
	got, _ := os.ReadFile(filepath.Join(dir, "room.err"))
	if !strings.Contains(string(got), "from the log package") {
		t.Errorf("room.err is %q", got)
	}
	if out, _ := os.ReadFile(filepath.Join(dir, "room.out")); string(out) != "to stdout" {
		t.Errorf("room.out is %q", out)
	}
}

func TestRedirectLogsLeavesAFileStderrAlone(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "handed"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	oldErr := os.Stderr
	os.Stderr = f
	defer func() { os.Stderr = oldErr }()

	dir := filepath.Join(t.TempDir(), "logs")
	if err := redirectLogs(dir, "room", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("a stderr that was already a file was redirected anyway")
	}
}
