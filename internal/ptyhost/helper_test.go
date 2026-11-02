package ptyhost

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The tests run a real pty with a runner they control: this test binary, re-executed as `__runner <mode> ...`.
//
//	echo                  prints READY, then GOT:<line> for each line typed. `quit` exits 0, `exit7` exits 7
//	tick                  prints T000001, T000002 ... every 10ms. `quit` exits 0
//	parent <pidfile>      starts a `sleeper` child, writes its pid to <pidfile>, then behaves like echo
//	burst <kb>            writes <kb> KB at once, then behaves like echo
//	burstgo <kb>          prints READY, waits for one byte, then writes <kb> KB and behaves like echo
//	burstexit <kb> <code> writes <kb> KB and exits at once with <code>
//
// And as `__host <stateDir> <idle>`, which is what the Start test launches detached.
func TestMain(m *testing.M) {
	// Cleared, these fall back to the live room's location file, which is how the f-011 spike found it. Pointed at a
	// file that does not exist, a runner started here can never find the room.
	missing := os.TempDir() + string(os.PathSeparator) + "atrium-ptyhost-test-no-such-location"
	for _, k := range []string{"ATRIUM_LOCATION", "ATRIUM_SHARED_LOCATION"} {
		os.Setenv(k, missing)
	}
	os.Unsetenv("ATRIUM_DEBUG_INPUTLAG")
	os.Unsetenv("ATRIUM_REAL_SCROLLBACK")
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "__runner":
			runnerMain(os.Args[2:])
			return
		case "__host":
			idle, _ := time.ParseDuration(os.Args[3])
			if err := Run(RunOptions{StateDir: os.Args[2], Build: "test-host", IdleExit: idle}); err != nil {
				fmt.Fprintln(os.Stderr, "host:", err)
				os.Exit(1)
			}
			return
		}
	}
	os.Exit(m.Run())
}

func runnerMain(args []string) {
	out := func(s string) { _, _ = os.Stdout.WriteString(s) }
	mode := args[0]
	atoi := func(i int) int { n, _ := strconv.Atoi(args[i]); return n }
	switch mode {
	case "burstexit":
		out(kb(atoi(1)))
		os.Exit(atoi(2))
	case "burstgo":
		out("READY\r\n")
		_, _ = os.Stdin.Read(make([]byte, 1))
		out(kb(atoi(1)))
		echoLoop(out)
	case "burst":
		out(kb(atoi(1)))
		echoLoop(out)
	case "parent":
		ch := exec.Command(os.Args[0], "__runner", "sleeper")
		if err := ch.Start(); err != nil {
			os.Exit(4)
		}
		_ = os.WriteFile(args[1], []byte(strconv.Itoa(ch.Process.Pid)), 0o600)
		echoLoop(out)
	case "sleeper":
		time.Sleep(10 * time.Minute) // wait: a child that outlives its runner until the test kills it
	case "tick":
		go echoLoop(out)
		for i := 1; ; i++ {
			out(fmt.Sprintf("T%06d\r\n", i))
			time.Sleep(10 * time.Millisecond) // wait: a runner that keeps writing is what is under test
		}
	default:
		echoLoop(out)
	}
}

// kb is n KB of numbered lines, so a gap or a repeat is visible.
func kb(n int) string {
	var b strings.Builder
	for i := 0; b.Len() < n*1024; i++ {
		fmt.Fprintf(&b, "B%07d ................................................\r\n", i)
	}
	return b.String()
}

func echoLoop(out func(string)) {
	out("READY\r\n")
	var line []byte
	one := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(one)
		if n == 0 || err != nil {
			os.Exit(3)
		}
		if one[0] != '\r' && one[0] != '\n' {
			line = append(line, one[0])
			continue
		}
		s := string(line)
		line = line[:0]
		switch s {
		case "":
		case "quit":
			out("bye\r\n")
			os.Exit(0)
		case "exit7":
			out("bye7\r\n")
			os.Exit(7)
		default:
			out("GOT:" + s + "\r\n")
		}
	}
}

// ---- harness ----
