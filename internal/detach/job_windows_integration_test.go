//go:build integration && windows

package detach

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const jobHelperEnv = "ATRIUM_DETACH_JOB_HELPER"

func TestDetachStartedInAJobThatRefusesBreakawayLeavesTheChildInNoJob(t *testing.T) {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skipf("no powershell.exe on PATH: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestJobHelperProcess$")
	cmd.Env = append(os.Environ(), jobHelperEnv+"=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	pidCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if rest, ok := strings.CutPrefix(sc.Text(), "CHILD="); ok {
				pidCh <- rest
				return
			}
			if rest, ok := strings.CutPrefix(sc.Text(), "SKIP="); ok {
				pidCh <- "skip:" + rest
				return
			}
		}
		pidCh <- "none"
	}()
	var got string
	select {
	case got = <-pidCh:
	case <-time.After(60 * time.Second):
		t.Fatal("the helper never reported a child")
	}
	if strings.HasPrefix(got, "skip:") {
		t.Skip(strings.TrimPrefix(got, "skip:"))
	}
	pid, err := strconv.Atoi(got)
	if err != nil {
		t.Fatalf("the helper answered %q", got)
	}
	if p, err := os.FindProcess(pid); err == nil {
		t.Cleanup(func() { _ = p.Kill() })
	}
	in, err := InJob(pid)
	if err != nil {
		t.Fatal(err)
	}
	if in {
		t.Fatalf("pid %d, started from a process in a job that refuses breakaway, is inside a job", pid)
	}
}

func TestJobHelperProcess(t *testing.T) {
	if os.Getenv(jobHelperEnv) == "" {
		t.Skip("run by the test above")
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		t.Fatal(err)
	}
	cur, _ := windows.GetCurrentProcess()
	if err := windows.AssignProcessToJobObject(job, cur); err != nil {
		os.Stdout.WriteString("SKIP=cannot put the helper in a job here: " + err.Error() + "\n")
		return
	}
	if in, _ := selfInJob(); !in {
		t.Fatal("the helper is in no job after assigning itself to one")
	}
	if probe, err := startOutsideJob(`C:\Windows\System32\PING.EXE`, []string{"-n", "1", "127.0.0.1"}, ""); err != nil {
		os.Stdout.WriteString("SKIP=WMI cannot create a process here: " + err.Error() + "\n")
		return
	} else {
		_ = probe.Kill()
	}
	p, err := Start(`C:\Windows\System32\PING.EXE`, []string{"-n", "30", "127.0.0.1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString("CHILD=" + strconv.Itoa(p.Pid) + "\n")
}
