//go:build windows && integration

package daemon

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/cardproc"
)

func init() {
	switch os.Getenv("ATRIUM_REAP_HELPER") {
	case "child":
		time.Sleep(time.Minute) // wait: a child that outlives the test until it is reaped
		os.Exit(0)
	case "parent":
		c := exec.Command(os.Args[0])
		c.Env = append(os.Environ(), "ATRIUM_REAP_HELPER=child")
		if err := c.Start(); err != nil {
			os.Exit(2)
		}
		os.Stdout.WriteString(strconv.Itoa(c.Process.Pid) + "\n")
		os.Exit(0)
	}
}

// What a runner started outlives it, and reapLeft ends it.
func TestReapLeftEndsWhatAnExitedRunnerLeft(t *testing.T) {
	started := time.Now().Add(-time.Second)
	c := exec.Command(os.Args[0])
	c.Env = append(os.Environ(), "ATRIUM_REAP_HELPER=parent")
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(out).ReadString('\n')
	_ = c.Wait()
	kid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("no child pid: %q", line)
	}
	if _, err := cardproc.StartTime(kid); err != nil {
		t.Fatalf("the child is not running before the reap: %v", err)
	}
	t.Cleanup(func() { _ = cardproc.StopTree(kid) })
	reapLeft(c.Process.Pid, started)
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) { // wait: poll until the reaped child is gone
		if _, err := cardproc.StartTime(kid); err != nil {
			return
		}
	}
	t.Fatal("the runner's child is still running after reapLeft")
}
