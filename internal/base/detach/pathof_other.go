//go:build !windows

package detach

import "os"

func PathOf(f *os.File) (string, bool) {
	if f == nil {
		return "", false
	}
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	return f.Name(), true
}
