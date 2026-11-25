//go:build integration

package runnersetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeminiAndQwenLaunchWireTheirHooksOnceInTheRowsHome(t *testing.T) {
	for _, c := range []struct {
		a    *Adapter
		dir  string
		file string
		want []string
	}{
		{Gemini, ".gemini", "settings.json", []string{"BeforeAgent", "AfterAgent", "BeforeTool", "Notification"}},
		{Qwen, ".qwen", "settings.json", []string{"UserPromptSubmit", "Stop", "PermissionRequest"}},
	} {
		t.Run(c.a.ID, func(t *testing.T) {
			env, _ := geminiHome(t)
			env.AtriumExe = "/opt/atrium/atrium"
			path := filepath.Join(env.Home, c.dir, c.file)
			writeFile(t, path, `{"theme":"dark"}`)

			note, err := c.a.Launch(env, env.Home)
			if err != nil || !strings.Contains(note, "wired atrium's hooks") {
				t.Fatalf("first launch: %q %v", note, err)
			}
			raw, _ := os.ReadFile(path)
			for _, hook := range append(c.want, `"theme"`, "--runner "+c.a.ID) {
				if !strings.Contains(string(raw), hook) {
					t.Errorf("%s has no %s:\n%s", path, hook, raw)
				}
			}
			if note, err := c.a.Launch(env, env.Home); err != nil || strings.Contains(note, "wired") {
				t.Fatalf("second launch should change nothing: %q %v", note, err)
			}
			if r := c.a.Checks[len(c.a.Checks)-1].Run(env); r.State != OK {
				t.Fatalf("after launch the hooks check is %+v", r)
			}
		})
	}
}

func TestAnUnreadableSettingsFileIsLeftAloneAtLaunch(t *testing.T) {
	env, _ := geminiHome(t)
	env.AtriumExe = "/opt/atrium/atrium"
	path := filepath.Join(env.Home, ".qwen", "settings.json")
	writeFile(t, path, `{not json`)
	if _, err := Qwen.Launch(env, env.Home); err == nil {
		t.Fatal("a file atrium cannot parse should be refused")
	}
	if raw, _ := os.ReadFile(path); string(raw) != `{not json` {
		t.Fatalf("the file was rewritten: %s", raw)
	}
}
