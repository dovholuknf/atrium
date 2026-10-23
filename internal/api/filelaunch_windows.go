package api

import (
	"golang.org/x/sys/windows"
)

// ShellExecute rather than `cmd /c start` or explorer: no command line is parsed, so
// nothing in a filename is read as a switch or a second command.
func osLaunch(path, dir string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	cwd, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, cwd, windows.SW_SHOWNORMAL)
}
