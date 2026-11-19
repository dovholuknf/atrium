//go:build integration

package claudeconf

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const userSettings = `{
  "model": "opus",
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "my-lint.sh", "timeout": 5}]},
      {"matcher": "", "hooks": [{"type": "command", "command": "my-audit.sh"}]}
    ],
    "Stop": [{"matcher": "", "hooks": [{"type": "command", "command": "notify-me"}]}]
  },
  "statusLine": {"type": "command", "command": "my-status"}
}`

func TestUninstallAfterInstallLeavesTheUsersSettingsAsTheyWere(t *testing.T) {
	path := withHome(t, userSettings)
	exe := "/opt/atrium/atrium.exe"
	if HasAtriumHooks(Claude) {
		t.Fatal("a file with only the user's hooks reads as having atrium's")
	}
	if _, _, err := Install(exe); err != nil {
		t.Fatal(err)
	}
	if !HasAtriumHooks(Claude) {
		t.Fatal("after install the file still reads as having no atrium hooks")
	}

	res, err := UninstallTarget(Claude)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Removed) == 0 || res.Backup == "" {
		t.Fatalf("uninstall removed nothing or kept no backup: %+v", res)
	}
	if _, err := os.Stat(res.Backup); err != nil {
		t.Fatalf("the backup is not there: %v", err)
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(userSettings), &want); err != nil {
		t.Fatal(err)
	}
	if got := readSettings(t, path); !reflect.DeepEqual(got, want) {
		t.Fatalf("the user's settings did not survive the round trip:\n got %v\nwant %v", got, want)
	}
	if HasAtriumHooks(Claude) {
		t.Fatal("after uninstall the file still reads as having atrium's hooks")
	}

	again, err := UninstallTarget(Claude)
	if err != nil || len(again.Removed) != 0 || again.Backup != "" {
		t.Fatalf("a second uninstall was not a no-op: %+v %v", again, err)
	}
}

func TestUninstallKeepsTheUsersEntriesInTheirOwnKeyOrder(t *testing.T) {
	path := withHome(t, `{"hooks":{"PreToolUse":[`+
		`{"matcher":"Bash","hooks":[{"type":"command","command":"my-lint.sh"}]},`+
		`{"matcher":"","hooks":[{"type":"command","command":"\"C:/Program Files/atrium/atrium.exe\" hook --event tool-start"},`+
		`{"command":"keep-me","type":"command"}]}],`+
		`"SessionStart":[{"matcher":"","hooks":[{"type":"command","command":"& \"C:/a b/atrium.exe\" session --event start"}]}]},`+
		`"statusLine":{"type":"command","command":"C:/x/atrium.exe statusline"}}`)
	res, err := UninstallTarget(Claude)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Removed) != 3 {
		t.Fatalf("want the two atrium hooks and the status line removed, got %v", res.Removed)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		t.Fatal(err)
	}
	got := compact.String()
	for _, keep := range []string{
		`{"matcher":"Bash","hooks":[{"type":"command","command":"my-lint.sh"}]}`,
		`{"command":"keep-me","type":"command"}`,
	} {
		if !strings.Contains(got, keep) {
			t.Fatalf("%s did not keep its bytes:\n%s", keep, got)
		}
	}
	for _, gone := range []string{"SessionStart", "statusLine", "tool-start"} {
		if strings.Contains(got, gone) {
			t.Fatalf("%s is still there:\n%s", gone, got)
		}
	}
	if b, _ := filepath.Glob(path + ".atrium-*.bak"); len(b) != 1 {
		t.Fatalf("want one backup, got %v", b)
	}
}

func TestUninstallWithNoSettingsFileWritesNothing(t *testing.T) {
	path := withHome(t, "")
	res, err := UninstallTarget(Claude)
	if err != nil || len(res.Removed) != 0 {
		t.Fatalf("got %+v %v", res, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("uninstall created a settings file")
	}
}
