package api

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Where new agents start and whether they work in place or in a copy. Both are only the new agent dialog's
// defaults: each launch can still pick another folder or the other way.
const (
	SettingLaunchFolder = "launch_folder"
	SettingLaunchMode   = "launch_mode"
)

// launchModeOf is the stored form of a launch_mode: "place", "copy", or empty for the dialog's own default.
func launchModeOf(v string) (string, error) {
	switch m := strings.ToLower(strings.TrimSpace(v)); m {
	case "", "place", "copy":
		return m, nil
	}
	return "", fmt.Errorf("launch_mode is place or copy, not %q", v)
}

// launchFolderOf is a launch_folder with forward slashes and no trailing one, as the dialog shows a folder.
func launchFolderOf(v string) string {
	f := filepath.ToSlash(strings.TrimSpace(v))
	if len(f) > 1 && !strings.HasSuffix(f, ":/") {
		f = strings.TrimRight(f, "/")
	}
	return f
}
