package roomspec

import (
	"errors"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

// OSFS is the machine this process runs on.
type OSFS struct{}

func (OSFS) Lstat(p string) (fs.FileInfo, error) { return os.Lstat(p) }
func (OSFS) ReadFile(p string) ([]byte, error)   { return os.ReadFile(p) }
func (OSFS) WriteFile(p string, d []byte, m fs.FileMode) error {
	return os.WriteFile(p, d, m)
}
func (OSFS) MkdirAll(p string, m fs.FileMode) error { return os.MkdirAll(p, m) }
func (OSFS) RemoveAll(p string) error               { return os.RemoveAll(p) }

// Examine asks for the attributes of the folder. On Unix that is a stat of "<p>/.", which needs search permission on p itself
// (a plain stat of p only needs it on p's parent). On Windows it is a stat, which needs FILE_READ_ATTRIBUTES.
func (OSFS) Examine(p string) Examine {
	q := p
	if runtime.GOOS != "windows" {
		q = strings.TrimRight(p, "/") + "/."
	}
	_, err := os.Stat(q)
	switch {
	case err == nil:
		return ExamineOK
	case errors.Is(err, fs.ErrNotExist):
		// a folder under one that cannot be searched also says "not exist" on some systems: look at the parent
		if par := filepath.Dir(p); par != p && runtime.GOOS != "windows" {
			if _, perr := os.Stat(par + "/."); perr != nil && errors.Is(perr, fs.ErrPermission) {
				return ExamineDenied
			}
		}
		return ExamineMissing
	default:
		return ExamineDenied
	}
}

// Writable is canWriteDir (access_unix.go, access_windows.go): the permission asked for, nothing written.
func (OSFS) Writable(dir string) bool { return canWriteDir(dir) }

// Resolve follows links, junctions and short names on the deepest ancestor of p that exists, and puts the rest back.
func (OSFS) Resolve(p string) string {
	cur, rest := filepath.FromSlash(p), ""
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			r = longPath(r)
			if rest != "" {
				r = filepath.Join(r, rest)
			}
			return strings.TrimPrefix(slash(filepath.ToSlash(r)), "//?/")
		}
		par := filepath.Dir(cur)
		if par == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = par
	}
}

// Home is this account's folders.
func (OSFS) Home() Home {
	h, _ := os.UserHomeDir()
	h = slash(h)
	cfg := h + "/.config"
	appsup := cfg
	switch runtime.GOOS {
	case "windows":
		if a := os.Getenv("APPDATA"); a != "" {
			cfg = slash(a)
		} else {
			cfg = h + "/AppData/Roaming"
		}
		appsup = cfg
	case "darwin":
		appsup = h + "/Library/Application Support"
		if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
			cfg = slash(x)
		}
	default:
		if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
			cfg = slash(x)
		}
		appsup = cfg
	}
	return Home{Dir: h, Config: cfg, AppSup: appsup}
}

// Login is the account the process runs as. Windows says DOMAIN\name, which is also what an ACL names.
func (OSFS) Login() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USER")
}
