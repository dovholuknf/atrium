//go:build integration

package capability

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	switch os.Getenv("CAP_HELPER") {
	case "ok":
		os.Stdout.WriteString("tool 1.2.3 ghp_abcdefghijklmnop1234\n")
		os.Exit(0)
	case "fail":
		os.Stderr.WriteString("nope\n")
		os.Exit(3)
	case "hang":
		time.Sleep(time.Minute) // wait: the hung hook the test kills
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helper(mode string) []string {
	os.Setenv("CAP_HELPER", mode)
	return []string{os.Args[0]}
}

func TestProbeJudgesAndScrubs(t *testing.T) {
	a := NewAuditor(Default(), t.TempDir(), "")
	ok := a.Probe(context.Background(), Cap{ID: "t", Kind: "toolchain", Probe: helper("ok"), Version: `tool (\S+)`})
	if ok.State != Pass || ok.Version != "1.2.3" || strings.Contains(ok.Reason, "ghp_") {
		t.Fatalf("ok probe: %+v", ok)
	}
	bad := a.Probe(context.Background(), Cap{ID: "t", Kind: "toolchain", Probe: helper("fail")})
	if bad.State != Fail || bad.Reason != "nope" {
		t.Fatalf("failing probe: %+v", bad)
	}
	gone := a.Probe(context.Background(), Cap{ID: "t", Kind: "toolchain", Probe: []string{"no-such-tool-here"}})
	if gone.State != Fail || !strings.Contains(gone.Reason, "not on PATH") {
		t.Fatalf("missing tool: %+v", gone)
	}
	slow := a.Probe(context.Background(), Cap{ID: "t", Kind: "toolchain", Probe: helper("hang"), Timeout: "300ms"})
	if slow.State != Fail || slow.Reason != "timeout" {
		t.Fatalf("hanging probe: %+v", slow)
	}
}

func TestRunOnlyDeclaredAndNeedsGate(t *testing.T) {
	l := List{Version: 1, Caps: []Cap{
		{ID: "base", Kind: "toolchain", Probe: []string{"no-such-tool-here"}},
		{ID: "top", Kind: "reach", Probe: helper("ok"), Needs: []string{"base"}},
	}}
	a := NewAuditor(l, t.TempDir(), t.TempDir()+"/r.json")
	if _, err := a.Run(context.Background(), []string{"rm -rf"}); err == nil {
		t.Fatal("an undeclared id ran")
	}
	rs, err := a.Run(context.Background(), []string{"top"})
	if err != nil || len(rs) != 2 {
		t.Fatalf("run: %v %+v", err, rs)
	}
	if rs[1].State != Unknown || rs[1].Reason != "needs base" {
		t.Fatalf("a failed need did not hold its dependant: %+v", rs[1])
	}
	again := NewAuditor(l, a.Scratch, a.File)
	if len(again.Snapshot().Results) != 2 {
		t.Fatal("results did not survive a restart")
	}
}

func TestProbeHardwareNeverFailsAndReadsTheBasics(t *testing.T) {
	h := ProbeHardware(context.Background(), t.TempDir()+"/not/made/yet")
	if h.Cores < 1 || h.RAM == 0 || h.DiskFree == 0 || h.At == "" {
		t.Fatalf("%+v", h)
	}
	if s := strings.Join(h.Summary(), "\n"); !strings.Contains(s, "cores") || !strings.Contains(s, "disk:") {
		t.Fatalf("summary: %s", s)
	}
}
