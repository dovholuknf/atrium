package roomspec

import (
	"io/fs"
	"os"
)

// Examine is what an account can do with a folder on the way to a path: read its attributes (Windows) or search it (Unix).
// Claude Code does this to every folder on the way to a path it writes, and raises a prompt nothing can answer when it cannot.
type Examine int

const (
	ExamineOK Examine = iota
	ExamineMissing
	ExamineDenied
)

// ReadFS is every question the plan may ask of a machine, and nothing else. A plan is given this, not a flag: it has no way to
// write, because there is no method to write with.
type ReadFS interface {
	Lstat(path string) (fs.FileInfo, error)
	ReadFile(path string) ([]byte, error)
	// Examine says whether the attributes of path can be read, without listing or creating anything.
	Examine(path string) Examine
	// Writable says whether a file could be made in dir, by reading the permission and not by making one.
	Writable(dir string) bool
	// Home is the account's files: profile, config, and the Mac's Application Support.
	Home() Home
	// Login is who this process runs as, as the OS spells it (SG3\localai, localai).
	Login() string
}

// FS is a machine as an apply sees it: the plan's view, and the means to change it.
type FS interface {
	ReadFS
	WriteFile(path string, data []byte, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
	RemoveAll(path string) error
}

// ReadEnv is the user environment, read only.
type ReadEnv interface {
	UserEnv(name string) (string, bool)
}

// Env is the user environment, which Windows keeps in the registry.
type Env interface {
	ReadEnv
	SetUserEnv(name, value string) error
}

// IsLink is whether a file is a symlink, or on Windows a junction or other reparse point (Go reports a junction as irregular).
func IsLink(fi fs.FileInfo) bool {
	return fi != nil && fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0
}
