//go:build windows

package daemon

import "github.com/dovholuknf/atrium/internal/daemon/shared"

func powershell() string { return shared.Powershell() }
