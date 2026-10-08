package roomspec

import (
	"path"
	"strings"
)

// Format is how a setting is written in its file.
type Format string

const (
	FormatKV      Format = "kv"      // key=value, one line (.npmrc, go's env file)
	FormatINI     Format = "ini"     // key = value inside a [section] (pip.conf, pip.ini)
	FormatProfile Format = "profile" // export KEY='value' in a shell profile, marked so it is found again
	FormatUserEnv Format = "userenv" // a user environment variable: the registry on Windows
)

// Home is where an account keeps its files, as the adapter spells it: forward slashes, no trailing slash.
type Home struct {
	Dir    string // the profile folder (~, %USERPROFILE%)
	Config string // where config lives: %APPDATA% on Windows, $XDG_CONFIG_HOME or ~/.config on Linux
	AppSup string // ~/Library/Application Support on a Mac, else Config
}

// CacheRow is one tool setting that moves a cache into the work root. A new tool is a row here, nothing else: the file per OS,
// the key, the format, and which folder of the layout it points at.
type CacheRow struct {
	Cache   string                         // the spec's name for it: npm, go, pip, cargo
	Tool    string                         // what the setting is called to a person
	Format  Format                         //
	File    func(os string, h Home) string // the config file, "" when the OS keeps it elsewhere (FormatUserEnv)
	Section string                         // FormatINI
	Key     string                         //
	Folder  string                         // the cache folder this row points at, under the cache root: npm, go-mod, go-build, ...
	// Query is the tool's own answer, run as `Query...` and read back, to see something overriding the file. Empty is none.
	Query []string
}

// ProfileMarker ends the line a FormatProfile row owns, so a rerun replaces it and never doubles it.
const ProfileMarker = "# atrium work root"

func home(os string, h Home, rel string) string { return path.Join(h.Dir, rel) }

// CacheRows is the table. Order is the order they are applied and reported.
var CacheRows = []CacheRow{
	{Cache: "npm", Tool: "npm", Format: FormatKV, Key: "cache", Folder: "npm", Query: []string{"npm", "config", "get", "cache"},
		File: func(os string, h Home) string { return home(os, h, ".npmrc") }},
	{Cache: "go", Tool: "go GOMODCACHE", Format: FormatKV, Key: "GOMODCACHE", Folder: "go-mod", Query: []string{"go", "env", "GOMODCACHE"},
		File: goEnvFile},
	{Cache: "go", Tool: "go GOCACHE", Format: FormatKV, Key: "GOCACHE", Folder: "go-build", Query: []string{"go", "env", "GOCACHE"},
		File: goEnvFile},
	{Cache: "pip", Tool: "pip", Format: FormatINI, Section: "global", Key: "cache-dir", Folder: "pip", Query: []string{"pip", "config", "get", "global.cache-dir"},
		File: func(os string, h Home) string {
			if os == Windows {
				return path.Join(h.Config, "pip", "pip.ini")
			}
			return path.Join(h.Config, "pip", "pip.conf")
		}},
	{Cache: "cargo", Tool: "cargo CARGO_HOME", Format: FormatUserEnv, Key: "CARGO_HOME", Folder: "cargo",
		File: func(os string, h Home) string {
			if os == Windows {
				return "" // the user's environment (the registry), not a file
			}
			return home(os, h, ".profile")
		}},
}

// goEnvFile is go's own environment file: `go env -w` writes it, and `go env` reads it.
func goEnvFile(os string, h Home) string {
	if os == Darwin {
		return path.Join(h.AppSup, "go", "env")
	}
	return path.Join(h.Config, "go", "env")
}

// CacheNames are the caches a spec may name.
func CacheNames() []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range CacheRows {
		if !seen[r.Cache] {
			seen[r.Cache] = true
			out = append(out, r.Cache)
		}
	}
	return out
}

// CacheByName is the rows for one cache name.
func CacheByName(name string) ([]CacheRow, bool) {
	var out []CacheRow
	for _, r := range CacheRows {
		if r.Cache == name {
			out = append(out, r)
		}
	}
	return out, len(out) > 0
}

// RowsFor is the rows of the named caches, in table order.
func RowsFor(names []string) []CacheRow {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var out []CacheRow
	for _, r := range CacheRows {
		if want[r.Cache] {
			out = append(out, r)
		}
	}
	return out
}

// FormatFor is how this row is written on an OS: a user environment variable is the registry on Windows and an export line in
// the profile everywhere else.
func (r CacheRow) FormatFor(os string) Format {
	if r.Format == FormatUserEnv && os != Windows {
		return FormatProfile
	}
	return r.Format
}

// Value is the folder a row points at, for resolved paths.
func (r CacheRow) Value(p Paths) string { return p.Cache + "/" + r.Folder }

// CacheDirs is every folder the named caches need under the cache root.
func CacheDirs(p Paths, names []string) []string {
	var out []string
	for _, r := range RowsFor(names) {
		out = append(out, r.Value(p))
	}
	return out
}

// Dirs is every folder the work root holds, in the order to make them: the root first.
func Dirs(p Paths, caches []string) []string {
	d := []string{p.Root, p.Git, p.Reviews, p.Handoff}
	if len(caches) > 0 {
		d = append(d, p.Cache)
	}
	d = append(d, CacheDirs(p, caches)...)
	// de-duplicated, keeping the first, in case a layout puts two names on one folder
	seen := map[string]bool{}
	var out []string
	for _, x := range d {
		k := strings.ToLower(x)
		if !seen[k] {
			seen[k] = true
			out = append(out, x)
		}
	}
	return out
}
