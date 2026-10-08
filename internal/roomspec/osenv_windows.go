//go:build windows

package roomspec

import "golang.org/x/sys/windows/registry"

// OSEnv is the user environment of this account: HKCU\Environment, which a process started later reads.
type OSEnv struct{}

func (OSEnv) UserEnv(name string) (string, bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return "", false
	}
	return v, true
}

func (OSEnv) SetUserEnv(name, value string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Environment`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, value)
}
