package cli

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/base/claudeconf"
)

func TestOneRowPerHookListsEachHookOnce(t *testing.T) {
	row := func(hook string, installed bool) claudeconf.HookStatus {
		return claudeconf.HookStatus{HookEvent: claudeconf.HookEvent{Hook: hook}, Installed: installed}
	}
	got := oneRowPerHook([]claudeconf.HookStatus{row("PreToolUse", true), row("PreToolUse", false), row("Stop", false)})
	if len(got) != 2 || got[0].Hook != "PreToolUse" || got[0].Installed || got[1].Hook != "Stop" {
		t.Fatalf("rows = %+v", got)
	}
}
