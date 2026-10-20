//go:build integration && windows

package detach

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	jobHelperEnv = "ATRIUM_DETACH_JOB_HELPER"
	jobLogEnv    = "ATRIUM_DETACH_JOB_LOG"
)

func TestDetachStartedInAJobThatRefusesBreakawayLeavesTheChildInNoJobThatBindsIt(t *testing.T) {
	requireWMI(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "child.log")
	cmd := exec.Command(exe, "-test.run=^TestJobHelperProcess$")
	cmd.Env = append(os.Environ(), jobHelperEnv+"=1", jobLogEnv+"="+log)
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
			if rest, ok := strings.CutPrefix(sc.Text(), "ERR="); ok {
				pidCh <- "err:" + rest
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
	if strings.HasPrefix(got, "err:") {
		t.Fatal(strings.TrimPrefix(got, "err:"))
	}
	pidStr, inHelper, _ := strings.Cut(got, " ")
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		t.Fatalf("the helper answered %q", got)
	}
	if p, err := os.FindProcess(pid); err == nil {
		t.Cleanup(func() { _ = p.Kill() })
	}
	if inHelper != "false" {
		t.Fatalf("pid %d, started from a process in a job that refuses breakaway, is in that job (%q)", pid, got)
	}
	deadline := time.Now().Add(20 * time.Second)
	var report string
	for {
		data, _ := os.ReadFile(log)
		if i := strings.Index(string(data), "JOB "); i >= 0 && strings.Contains(string(data)[i:], "\n") {
			report = strings.TrimSpace(string(data)[i:])
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the child never reported its job, its log has %q", data)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Log(report)
	if strings.Contains(report, "limits=true") {
		t.Fatalf("pid %d is in a job that ends or limits it: %s", pid, report)
	}
}

func TestJobReportProcess(t *testing.T) {
	if !slices.Contains(os.Args, "-test.run=^TestJobReportProcess$") {
		t.Skip("started by the test above")
	}
	in, _, detail := SelfJob()
	os.Stdout.WriteString("JOB in=" + strconv.FormatBool(in) + " " + detail + "\n")
	time.Sleep(30 * time.Second)
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
		os.Stdout.WriteString("ERR=cannot put the helper in a job here: " + err.Error() + "\n")
		return
	}
	if in, _ := selfInJob(); !in {
		t.Fatal("the helper is in no job after assigning itself to one")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p, err := Start(exe, []string{"-test.run=^TestJobReportProcess$"}, os.Getenv(jobLogEnv))
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(p.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	var inHelper int32
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")
	if r, _, err := proc.Call(uintptr(h), uintptr(job), uintptr(unsafe.Pointer(&inHelper))); r == 0 {
		t.Fatal(err)
	}
	os.Stdout.WriteString("CHILD=" + strconv.Itoa(p.Pid) + " " + strconv.FormatBool(inHelper != 0) + "\n")
}
