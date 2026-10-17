package rolebin

import (
	"bytes"
	"crypto/sha256"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var roleNames = map[string]string{
	"hub":  "atrium-hub",
	"room": "atrium-room",
	"pty":  "atrium-pty",
}

var legacyNames = []string{"atrium.ptyhost"}

var rename = os.Rename

var logOnce sync.Once

func ext() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// Running is this process's own image.
func Running() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	return self
}

// Installed is the canonical binary: Running with a role name mapped back to atrium in the same directory.
func Installed() string {
	return installedFrom(Running())
}

func installedFrom(self string) string {
	if self == "" {
		return ""
	}
	dir, base := filepath.Split(self)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	isRole := false
	for _, n := range roleNames {
		if stem == n {
			isRole = true
		}
	}
	for _, n := range legacyNames {
		if stem == n || base == n {
			isRole = true
		}
	}
	if !isRole {
		return self
	}
	canon := filepath.Join(dir, "atrium"+filepath.Ext(base))
	if _, err := os.Stat(canon); err == nil {
		return canon
	}
	return self
}

// RoleFile is the path the copy for role has beside exe.
func RoleFile(role, exe string) string {
	name, ok := roleNames[role]
	if !ok {
		return exe
	}
	return filepath.Join(filepath.Dir(exe), name+filepath.Ext(exe))
}

// Ensure returns the role file, current, or Installed when anything fails.
func Ensure(role string) string {
	exe := Installed()
	return EnsureIn(role, exe, filepath.Dir(exe))
}

// EnsureIn is Ensure for a given installed binary and a given directory for the copy.
func EnsureIn(role, exe, dir string) string {
	if os.Getenv("ATRIUM_ROLEBIN") == "off" {
		return exe
	}
	name, ok := roleNames[role]
	if !ok || exe == "" {
		return exe
	}
	got, err := ensure(role, name, exe, dir)
	if err != nil {
		logOnce.Do(func() { log.Printf("rolebin: %s copy of %s unavailable, using the binary itself: %v", role, exe, err) })
		return exe
	}
	return got
}

func ensure(role, name, exe, dir string) (string, error) {
	dst := filepath.Join(dir, name+filepath.Ext(exe))
	if role != "pty" {
		sweepOld(dir, name)
	}
	if current(exe, dst) {
		return dst, nil
	}
	tmp := dst + ".new"
	if err := copyFile(exe, tmp); err != nil {
		_ = os.Remove(tmp)
		return keepOrFail(dst, err)
	}
	if role != "pty" {
		if _, err := os.Stat(dst); err == nil {
			old := filepath.Join(dir, name+".old"+filepath.Ext(exe))
			_ = os.Remove(old)
			if err := rename(dst, old); err != nil {
				_ = os.Remove(tmp)
				return keepOrFail(dst, err)
			}
		}
	}
	if err := rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return keepOrFail(dst, err)
	}
	return dst, nil
}

// keepOrFail uses a copy that cannot be replaced as it is, since a stale copy still speaks what its peer checks.
func keepOrFail(dst string, err error) (string, error) {
	if _, statErr := os.Stat(dst); statErr == nil {
		return dst, nil
	}
	return "", err
}

func sweepOld(dir, name string) {
	matches, _ := filepath.Glob(filepath.Join(dir, name+".old*"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// RemoveLegacy deletes the pre-role copies in dir, which fails quietly while a process still holds one.
func RemoveLegacy(dir string) {
	for _, n := range legacyNames {
		for _, f := range []string{n, n + ".exe"} {
			_ = os.Remove(filepath.Join(dir, f))
		}
	}
}

func current(src, dst string) bool {
	si, err := os.Stat(src)
	if err != nil {
		return false
	}
	di, err := os.Stat(dst)
	if err != nil || di.Size() != si.Size() {
		return false
	}
	a, err := hash(src)
	if err != nil {
		return false
	}
	b, err := hash(dst)
	return err == nil && bytes.Equal(a, b)
}

func hash(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
