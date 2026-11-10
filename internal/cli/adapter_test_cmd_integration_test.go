//go:build integration

package cli

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Today's claude hooks pass the kit unchanged. That is the standing proof that v1 describes what runners already do.
func TestTodaysClaudeHooksPassTheKit(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "atrium")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	build := exec.Command("go", "build", "-o", exe, "github.com/dovholuknf/atrium/cmd/atrium")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatal(err)
	}
	k := adapterKit{exe: exe, timeout: 60 * time.Second, out: os.Stderr}
	results := k.run()
	for _, r := range results {
		if len(r.faults) != 0 {
			t.Errorf("%s: %v", r.scenario, r.faults)
		}
	}
	for _, f := range []string{"session", "activity", "permission", "stop"} {
		if !results[0].features[f] {
			t.Errorf("claude's hooks never made the %s call", f)
		}
	}
}

// TestAdapterHelper is the adapter the kit tests run, re-executed from the test binary. ADAPTER_HELPER says how it
// behaves: "good" fails open on everything, "strict" exits 1 when the room is not answering, "liar" claims a feature
// it never uses and "badbody" sends a session start the schema refuses.
func TestAdapterHelper(t *testing.T) {
	mode := os.Getenv("ADAPTER_HELPER")
	if mode == "" {
		t.Skip("run by the kit")
	}
	loc, err := os.ReadFile(os.Getenv("ATRIUM_LOCATION"))
	if err != nil {
		os.Exit(3)
	}
	base := strings.Split(strings.Split(string(loc), `"agent":"`)[1], `"`)[0]
	client := &http.Client{Timeout: time.Second}
	post := func(path, body string) bool {
		resp, err := client.Post(base+path, "application/json", strings.NewReader(body))
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode == http.StatusOK
	}
	start := `{"agent":"kit-agent","task_id":"kit-card","event":"start","runner":"fake","contract":1}`
	switch mode {
	case "liar":
		start = `{"agent":"kit-agent","event":"start","features":["session","permission"]}`
	case "badbody":
		start = `{"agent":"kit-agent","event":"launch","pid":"one"}`
	}
	ok := post("/session", start)
	ok = post("/activity", `{"agent":"kit-agent","event":"tool-start","tool":"Bash"}`) && ok
	if mode != "liar" {
		ok = post("/permission", `{"agent":"kit-agent","tool":"Bash","command":"rm kit"}`) && ok
	}
	if mode == "strict" && !ok {
		os.Exit(1)
	}
}

func adapterHelperKit(t *testing.T, mode string) []scenarioResult {
	t.Helper()
	t.Setenv("ADAPTER_HELPER", mode)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	k := adapterKit{cmd: self + " -test.run=TestAdapterHelper", timeout: 20 * time.Second, out: io.Discard}
	return k.run()
}

func faultsOf(results []scenarioResult) map[string][]string {
	out := map[string][]string{}
	for _, r := range results {
		out[r.scenario] = r.faults
	}
	return out
}

func TestKitPassesAnAdapterThatFailsOpen(t *testing.T) {
	for sc, faults := range faultsOf(adapterHelperKit(t, "good")) {
		if len(faults) != 0 {
			t.Errorf("%s: %v", sc, faults)
		}
	}
}

func TestKitFailsAnAdapterThatExitsWhenTheRoomIsNotThere(t *testing.T) {
	got := faultsOf(adapterHelperKit(t, "strict"))
	if len(got[scenarioOK]) != 0 {
		t.Fatalf("ok: %v", got[scenarioOK])
	}
	for _, sc := range []string{scenarioRefuse, scenarioHang} {
		if len(got[sc]) == 0 {
			t.Errorf("%s: an adapter that exits 1 passed", sc)
		}
	}
}

func TestKitHoldsAnAdapterToTheFeaturesItClaimed(t *testing.T) {
	got := faultsOf(adapterHelperKit(t, "liar"))[scenarioOK]
	if len(got) != 1 || !strings.Contains(got[0], "claimed permission") {
		t.Fatalf("faults: %v", got)
	}
}

func TestKitRefusesABodyThatBreaksTheSchema(t *testing.T) {
	got := strings.Join(faultsOf(adapterHelperKit(t, "badbody"))[scenarioOK], "\n")
	for _, want := range []string{"/event", "/pid"} {
		if !strings.Contains(got, want) {
			t.Errorf("no fault at %s in %q", want, got)
		}
	}
}

func TestKitTimesOutAnAdapterThatHangs(t *testing.T) {
	room := newKitRoom(scenarioHang)
	defer room.close()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	if _, err := client.Get(room.srv.URL + "/gate"); err == nil {
		t.Fatal("a hanging room answered")
	}
	resp, err := client.Post(room.srv.URL+"/permission", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("a hanging room must still answer a permission long poll: %v", err)
	}
	resp.Body.Close()
}
