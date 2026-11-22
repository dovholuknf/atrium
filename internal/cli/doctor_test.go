package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/setupcheck"
)

func fakeLocal() setupcheck.Report {
	var r setupcheck.Report
	r.OS = "linux"
	r.Add(setupcheck.Result{ID: "tool.git", State: setupcheck.Warn, Detail: "git is missing", Fix: setupcheck.FixExplain, Command: "apt install git"},
		setupcheck.Result{ID: "binary.installed", State: setupcheck.Fail, Detail: "in a build folder", Fix: setupcheck.FixExplain},
		setupcheck.Result{ID: "account.standard", State: setupcheck.OK, Detail: "agent: fine", Fix: ""})
	return r
}

func TestDoctorWithNoRoomReadsLocallyAndSaysSo(t *testing.T) {
	rep := doctorReport("http://127.0.0.1:1", fakeLocal)
	if rep.Checks[0].ID != "room.running" || rep.Checks[0].State != setupcheck.Warn || rep.Checks[0].Command != "atrium run" {
		t.Fatalf("first row: %+v", rep.Checks[0])
	}
	if rep.Failing != 1 || rep.Warning != 2 || len(rep.Checks) != 4 {
		t.Errorf("counts: %d failing %d warning %d rows", rep.Failing, rep.Warning, len(rep.Checks))
	}
}

func TestDoctorPrefersTheRoomsAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/setup" || r.Method != http.MethodGet {
			t.Errorf("doctor asked %s %s", r.Method, r.URL.Path)
		}
		var rep setupcheck.Report
		rep.Add(setupcheck.Result{ID: "account.standard", State: setupcheck.OK, Detail: "localai: fine"})
		_ = json.NewEncoder(w).Encode(rep)
	}))
	defer srv.Close()
	rep := doctorReport(srv.URL, func() setupcheck.Report { t.Error("local ran with a room up"); return setupcheck.Report{} })
	if rep.Checks[0].ID != "room.running" || rep.Checks[0].State != setupcheck.OK || len(rep.Checks) != 2 {
		t.Errorf("%+v", rep.Checks)
	}
}

func TestDoctorPrintsWhoFixesEachRowAndFailsOnAFail(t *testing.T) {
	var buf bytes.Buffer
	printDoctor(&buf, fakeLocal())
	got := buf.String()
	for _, want := range []string{"warn  tool.git", "fix: you run this: apt install git", "fail  binary.installed", "ok    account.standard", "1 failing, 1 warning, 3 checks on linux"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "agent: fine\n      fix") {
		t.Error("an ok row prints no fix")
	}
}

func TestDoctorHasNoFlagThatWrites(t *testing.T) {
	c := newDoctor()
	for _, n := range []string{"fix", "install", "apply", "yes", "force", "restart"} {
		if c.Flags().Lookup(n) != nil {
			t.Errorf("doctor has --%s", n)
		}
	}
	if helpGroup("doctor") != "start" {
		t.Errorf("doctor is in %q", helpGroup("doctor"))
	}
}
