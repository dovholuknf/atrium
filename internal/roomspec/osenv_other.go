//go:build !windows

package roomspec

import "errors"

// OSEnv is the user environment. Unix has no such store: a row that wants one is written to the profile instead (see
// CacheRow.FormatFor), so this is never asked.
type OSEnv struct{}

func (OSEnv) UserEnv(string) (string, bool) { return "", false }
func (OSEnv) SetUserEnv(string, string) error {
	return errors.New("this OS keeps no user environment store: the profile is written instead")
}
