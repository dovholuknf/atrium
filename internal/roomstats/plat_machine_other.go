//go:build !windows && !linux && !darwin

package roomstats

import "errors"

// ReadMachine has no reading on this platform, so the section stays absent.
func ReadMachine() (MachineReading, error) {
	return MachineReading{}, errors.New("machine stats not supported on this platform")
}
