//go:build !windows

package cli

import "errors"

func runAsService([]string) (int, bool) { return 0, false }

var errServiceWindowsOnly = errors.New("a service is a Windows thing. on Linux use packaging/atrium.service, on macOS packaging/atrium.plist")

func installService(serviceOpts) error     { return errServiceWindowsOnly }
func uninstallService(string) error        { return errServiceWindowsOnly }
func statusService(string) error           { return errServiceWindowsOnly }
func passwordService(string, string) error { return errServiceWindowsOnly }
