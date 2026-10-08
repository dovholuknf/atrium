package claudeconf

import (
	"encoding/json"
	"os"
)

// CompactSettings is what Claude Code's own settings files say about when it compacts a conversation.
type CompactSettings struct {
	// WindowK is `autoCompactWindow`, in thousands of tokens, or 0 when no file sets it.
	WindowK int
	// Off is `autoCompactEnabled: false`, so Claude Code never compacts on its own.
	Off bool
}

// AutoCompact reads `autoCompactWindow` and `autoCompactEnabled` from the settings files SettingsPaths names,
// the narrowest file last so it wins. A file that is missing or cannot be parsed says nothing. This is the one
// place atrium can see the runner's own compaction point, since the flag atrium starts it with is not the only
// thing that moves it: a settings file on the machine that sets a smaller window wins.
func AutoCompact(projectDir string) CompactSettings {
	var out CompactSettings
	for _, p := range SettingsPaths(projectDir) {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var f struct {
			Window  *float64 `json:"autoCompactWindow"`
			Enabled *bool    `json:"autoCompactEnabled"`
		}
		if json.Unmarshal(b, &f) != nil {
			continue
		}
		if f.Window != nil && *f.Window >= 1000 {
			out.WindowK = int(*f.Window / 1000)
		}
		if f.Enabled != nil {
			out.Off = !*f.Enabled
		}
	}
	return out
}
