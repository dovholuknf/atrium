package cli

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// readClipboard is the text on the system clipboard, read with the platform's own tool: PowerShell on Windows,
// pbpaste on a Mac, and wl-paste, xclip or xsel on Linux, whichever is installed.
func readClipboard() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var tries [][]string
	switch runtime.GOOS {
	case "windows":
		tries = [][]string{{"powershell", "-NoProfile", "-NonInteractive", "-Command", "Get-Clipboard -Raw"}}
	case "darwin":
		tries = [][]string{{"pbpaste"}}
	default:
		tries = [][]string{{"wl-paste", "--no-newline"}, {"xclip", "-selection", "clipboard", "-o"},
			{"xsel", "--clipboard", "--output"}}
	}
	for _, t := range tries {
		if _, err := exec.LookPath(t[0]); err != nil {
			continue
		}
		out, err := exec.CommandContext(ctx, t[0], t[1:]...).Output()
		if err != nil {
			continue
		}
		return strings.TrimSpace(string(out)), nil
	}
	return "", errors.New("could not read the clipboard here. pass the url instead")
}
