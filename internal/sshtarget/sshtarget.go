// Package sshtarget is the list of TCP addresses this room offers to other rooms, the file `ssh_targets.json` beside
// the room's key. See docs/rnd/ssh-across-rooms.md.
//
// ATRIUM HOLDS THE NAME OF A TARGET AND WHICH ROOMS MAY REACH IT, NEVER A KEY, PASSWORD OR AGENT SOCKET. The login is
// sshd's own. Nothing here writes the file except Add, Remove and Save, which `atrium ssh` calls.
package sshtarget

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// File is the target list's name inside the room's key directory.
const File = "ssh_targets.json"

// Version is the only file version this reads.
const Version = 1

// MaxTargets bounds the list.
const MaxTargets = 64

// Target is one address this room offers. User is a hint for the generated ssh config, not a credential.
type Target struct {
	Name string   `json:"name"`
	Addr string   `json:"addr"`
	From []string `json:"from"`
	User string   `json:"user"`
	Note string   `json:"note"`
}

// Set is the whole file.
type Set struct {
	Version int      `json:"version"`
	Targets []Target `json:"targets"`
}

var (
	nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	fromRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	userRE = regexp.MustCompile(`^[A-Za-z0-9._-]{0,64}$`)
)

// Validate says in a sentence what is wrong with t, or nil.
func Validate(t Target) error {
	if !nameRE.MatchString(t.Name) {
		return fmt.Errorf("name %q must be 1 to 32 characters of a-z, 0-9 and -, starting with a letter or digit", t.Name)
	}
	host, port, err := net.SplitHostPort(t.Addr)
	if err != nil || host == "" {
		return fmt.Errorf("addr %q must be host:port", t.Addr)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("addr %q has a port that is not 1 to 65535", t.Addr)
	}
	for _, f := range t.From {
		if f != "*" && !fromRE.MatchString(f) {
			return fmt.Errorf("from entry %q must be * or a room name", f)
		}
	}
	if !userRE.MatchString(t.User) {
		return fmt.Errorf("user %q must be at most 64 characters of letters, digits, . _ and -", t.User)
	}
	if utf8.RuneCountInString(t.Note) > 200 || strings.ContainsAny(t.Note, "\r\n") {
		return errors.New("note must be one line of at most 200 characters")
	}
	return nil
}

// Path is where the list lives under dir.
func Path(dir string) string { return filepath.Join(dir, File) }

// Load reads the list. A missing file is an empty set.
func Load(dir string) (Set, error) {
	b, err := os.ReadFile(Path(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return Set{Version: Version}, nil
	}
	if err != nil {
		return Set{}, err
	}
	var s Set
	if err := json.Unmarshal(b, &s); err != nil {
		return Set{}, fmt.Errorf("%s is not valid: %w", Path(dir), err)
	}
	if s.Version != Version {
		return Set{}, fmt.Errorf("%s has version %d and this reads version %d", Path(dir), s.Version, Version)
	}
	for _, t := range s.Targets {
		if err := Validate(t); err != nil {
			return Set{}, fmt.Errorf("%s, target %q: %w", Path(dir), t.Name, err)
		}
	}
	return s, nil
}

// Save writes the list through a temp file and a rename, mode 0600.
func Save(dir string, s Set) error {
	s.Version = Version
	if s.Targets == nil {
		s.Targets = []Target{}
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, File+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(append(b, '\n'))
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp, 0o600)
	}
	if werr == nil {
		werr = os.Rename(tmp, Path(dir))
	}
	if werr != nil {
		os.Remove(tmp)
	}
	return werr
}

// Add saves t. A name already there is an error unless replace is set.
func Add(dir string, t Target, replace bool) error {
	if err := Validate(t); err != nil {
		return err
	}
	if t.From == nil {
		t.From = []string{}
	}
	s, err := Load(dir)
	if err != nil {
		return err
	}
	for i := range s.Targets {
		if s.Targets[i].Name == t.Name {
			if !replace {
				return fmt.Errorf("target %q exists. --replace changes it", t.Name)
			}
			s.Targets[i] = t
			return Save(dir, s)
		}
	}
	if len(s.Targets) >= MaxTargets {
		return fmt.Errorf("a room offers at most %d targets", MaxTargets)
	}
	s.Targets = append(s.Targets, t)
	return Save(dir, s)
}

// Remove deletes the named target. A name that is not there is an error.
func Remove(dir, name string) error {
	s, err := Load(dir)
	if err != nil {
		return err
	}
	for i := range s.Targets {
		if s.Targets[i].Name == name {
			s.Targets = append(s.Targets[:i], s.Targets[i+1:]...)
			return Save(dir, s)
		}
	}
	return fmt.Errorf("no target named %q", name)
}

// Find returns the named target.
func Find(s Set, name string) (Target, bool) {
	for _, t := range s.Targets {
		if t.Name == name {
			return t, true
		}
	}
	return Target{}, false
}

// Allows says whether room from may open t: the list holds * or holds the name, ignoring case.
func Allows(t Target, from string) bool {
	for _, f := range t.From {
		if f == "*" || strings.EqualFold(f, from) {
			return true
		}
	}
	return false
}
