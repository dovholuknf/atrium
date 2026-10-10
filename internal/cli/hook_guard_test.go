package cli

import (
	"errors"
)

// guardFakeEnv answers the guard without touching the machine.
type guardFakeEnv struct {
	vars  map[string]string
	files map[string]string
}

func (guardFakeEnv) Git(string, ...string) (string, error) { return "", errors.New("no git here") }
func (guardFakeEnv) Stat(string) (bool, bool)              { return false, false }
func (guardFakeEnv) HasPrefix(string, string) bool         { return false }
func (e guardFakeEnv) ReadFile(p string) ([]byte, error) {
	if s, ok := e.files[p]; ok {
		return []byte(s), nil
	}
	return nil, errors.New("no such file")
}
func (e guardFakeEnv) Getenv(k string) string { return e.vars[k] }
func (guardFakeEnv) Agent() string            { return "" }
