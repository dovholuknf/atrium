//go:build integration

package cli

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"regexp"
	"testing"
)

func TestRoomJoinRefusesATakenPortBeforeWritingAndNamesTheRerun(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	free, release, err := freePort(0)
	if err != nil {
		t.Fatal(err)
	}
	release()
	agent := fmt.Sprintf("127.0.0.1:%d", free)

	dir := t.TempDir()
	var out bytes.Buffer
	c := joinCmd()
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs([]string{"JOINSTRING", "--dir", dir, "--isolated", "--http", held.Addr().String(), "--agent", agent})
	if err := c.Execute(); err == nil {
		t.Fatal("join went on with its --http port taken")
	}

	m := regexp.MustCompile(`atrium room join JOINSTRING --dir (\S+) --isolated --http (\S+) --agent (\S+)\n`).FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("no rerun line keeping --dir and --isolated in:\n%s", out.String())
	}
	if m[1] != dir {
		t.Fatalf("the rerun says --dir %s, want %s", m[1], dir)
	}
	m = m[1:]
	if m[1] == held.Addr().String() {
		t.Fatalf("suggested the taken port %s", m[1])
	}
	if m[2] != agent {
		t.Fatalf("--agent %s was free and should be kept, got %s", agent, m[2])
	}
	if ln, err := net.Listen("tcp", m[1]); err != nil {
		t.Fatalf("suggested --http %s is not free: %v", m[1], err)
	} else {
		ln.Close()
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("join wrote %d files before refusing", len(ents))
	}
}
