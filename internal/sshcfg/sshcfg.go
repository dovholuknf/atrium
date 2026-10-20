// Package sshcfg writes the ssh config blocks that reach a room through the hub. See docs/rnd/ssh-via-hub.md.
//
// IT IS THE ONE OPT-IN EXCEPTION to atrium never editing ~/.ssh/config, and it is narrow. It writes its OWN file,
// ~/.ssh/atrium_config, and adds exactly one line, `Include atrium_config`, at the top of ~/.ssh/config when that line
// is missing, after a timestamped backup. It never touches another line of that file, and removing blocks never removes
// the Include. A person asks for it with `atrium ssh config add`. Nothing else here is ever run on its own.
package sshcfg

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// File is atrium's own file under ~/.ssh, and IncludeLine the one line added to the user's config.
const (
	File        = "atrium_config"
	IncludeLine = "Include atrium_config"
)

// ProxyCommand is how ssh runs atrium for a room. Windows OpenSSH runs ProxyCommand without a shell, so the absolute
// path is quoted. On every other OS ssh runs it through the user's shell, so the path is plain, quoted only when it
// holds a space or a character the shell reads.
func ProxyCommand(exe, goos string) string { return ProxyCommandHub(exe, goos, "") }

// ProxyCommandHub is ProxyCommand with `--hub <hub>` when hub is not empty: the hub's board is somewhere other than
// the default, and the proxy would not find it on its own.
func ProxyCommandHub(exe, goos, hub string) string {
	tail := " ssh-proxy"
	if hub != "" {
		tail += " --hub " + shellWord(hub, goos)
	}
	tail += " %n"
	if goos == "windows" {
		return `"` + exe + `"` + tail
	}
	return shellWord(exe, goos) + tail
}

// shellWord quotes a word for the shell ssh runs ProxyCommand through, when it holds a character the shell reads.
// Windows runs it without a shell and the word is left alone.
func shellWord(w, goos string) string {
	if goos != "windows" && strings.ContainsAny(w, " \t'\"\\$`&;|<>()*?[]{}#~!") {
		return "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
	}
	return w
}

// Block is the Host block for one room, the same text `atrium ssh-proxy --print-config` prints. No port is opened on
// this machine: ProxyCommand speaks stdio. HostKeyAlias keeps the room's host key apart from a LAN entry for the same
// machine.
func Block(room, exe, goos string) string { return BlockHub(room, exe, goos, "") }

// BlockHub is Block with the hub's board named in the ProxyCommand when it is not the default.
func BlockHub(room, exe, goos, hub string) string { return BlockHubUser(room, exe, goos, hub, "") }

// BlockHubUser is BlockHub with `User <user>` when user is not empty: the login the room published to the hub, so
// `ssh <room>` needs no user typed.
func BlockHubUser(room, exe, goos, hub, user string) string {
	u := ""
	if user != "" {
		u = "  User " + user + "\n"
	}
	return "Host " + room + Suffix + "\n" + u +
		"  ProxyCommand " + ProxyCommandHub(exe, goos, hub) + "\n" +
		"  HostKeyAlias " + room + Suffix + "\n" +
		"  ServerAliveInterval 30\n" +
		"  ServerAliveCountMax 3\n" +
		"  CheckHostIP no\n"
}

// Suffix is what follows a room's name in its Host line and its HostKeyAlias, so `ssh sg3.atrium` reaches the room and
// cannot be mistaken for a LAN host named sg3. ProxyCommand's %n carries it to `atrium ssh-proxy`, which drops it.
const Suffix = ".atrium"

// migrateHostName rewrites a bare `Host <room>` line, from before the suffix, to `Host <room>.atrium`.
func migrateHostName(b block) string {
	lines := strings.SplitAfter(b.text, "\n")
	for i, l := range lines {
		t := strings.TrimRight(l, "\r\n")
		if t == "Host "+b.room {
			lines[i] = t + Suffix + l[len(t):]
		}
	}
	return strings.Join(lines, "")
}

// migrateHostToName rewrites a ProxyCommand written with %h to %n. %h is the HostName ssh resolved, which a LAN entry
// for the same Host changes into a name no room has, and %n is the name as typed.
func migrateHostToName(text string) string {
	lines := strings.SplitAfter(text, "\n")
	for i, l := range lines {
		t := strings.TrimRight(l, "\r\n")
		if strings.HasPrefix(strings.TrimSpace(t), "ProxyCommand ") && strings.Contains(t, " ssh-proxy ") && strings.HasSuffix(t, " %h") {
			lines[i] = t[:len(t)-2] + "%n" + l[len(t):]
		}
	}
	return strings.Join(lines, "")
}

var roomRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidRoom is a name safe to write into a Host line.
func ValidRoom(room string) error {
	if !roomRE.MatchString(room) {
		return fmt.Errorf("%q is not a room name", room)
	}
	return nil
}

// Result says what a call changed, so the command can say it in words.
type Result struct {
	Changed bool
	// Include is true when the Include line was added to the user's config by this call.
	Include bool
	// Created is true when ~/.ssh/config did not exist and was created with just that line.
	Created bool
	// Backup is the timestamped copy of ~/.ssh/config, empty when none was needed.
	Backup string
}

// Paths are the two files, for a home directory.
func Paths(home string) (dir, config, own string) {
	dir = filepath.Join(home, ".ssh")
	return dir, filepath.Join(dir, "config"), filepath.Join(dir, File)
}

const (
	beginPrefix = "# BEGIN atrium "
	endPrefix   = "# END atrium "
	header      = "# Written by `atrium ssh config`. Edit with that command, not by hand.\n"
)

type block struct{ room, text string }

// parse reads atrium_config back into its blocks, in order.
func parse(b []byte) []block {
	var out []block
	var cur *block
	for _, line := range strings.SplitAfter(string(b), "\n") {
		t := strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(t, beginPrefix):
			out = append(out, block{room: strings.TrimSpace(strings.TrimPrefix(t, beginPrefix))})
			cur = &out[len(out)-1]
		case strings.HasPrefix(t, endPrefix):
			cur = nil
		case cur != nil:
			cur.text += line
		}
	}
	return out
}

func render(blocks []block) []byte {
	var sb strings.Builder
	sb.WriteString(header)
	for _, b := range blocks {
		sb.WriteString("\n" + beginPrefix + b.room + "\n" + b.text + endPrefix + b.room + "\n")
	}
	return []byte(sb.String())
}

func readOwn(home string) ([]block, error) {
	_, _, own := Paths(home)
	b, err := os.ReadFile(own)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parse(b), nil
}

// Add writes the blocks for rooms into ~/.ssh/atrium_config, replacing a block that is already there for the same
// room, and makes sure the user's config includes that file. Calling it again with the same input changes nothing,
// makes no second backup and writes no second Include.
func Add(home, exe, goos string, rooms []string) (Result, error) {
	return AddHub(home, exe, goos, "", rooms)
}

// AddHub is Add with the hub's board written into each ProxyCommand when hub is not empty.
func AddHub(home, exe, goos, hub string, rooms []string) (Result, error) {
	return AddHubUsers(home, exe, goos, hub, rooms, nil)
}

var userRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`)

// AddHubUsers is AddHub with each room's login user, by room name, written into its block. A room with no entry gets
// no User line.
func AddHubUsers(home, exe, goos, hub string, rooms []string, users map[string]string) (Result, error) {
	var res Result
	for _, r := range rooms {
		if err := ValidRoom(r); err != nil {
			return res, err
		}
		if u := users[r]; u != "" && !userRE.MatchString(u) {
			return res, fmt.Errorf("%q is not a login name", u)
		}
	}
	have, err := readOwn(home)
	if err != nil {
		return res, err
	}
	next := append([]block(nil), have...)
	for i := range next {
		next[i].text = migrateHostToName(migrateHostName(next[i]))
	}
	for _, r := range rooms {
		nb := block{room: r, text: BlockHubUser(r, exe, goos, hub, users[r])}
		found := false
		for i := range next {
			if strings.EqualFold(next[i].room, r) {
				next[i], found = nb, true
			}
		}
		if !found {
			next = append(next, nb)
		}
	}
	dir, _, own := Paths(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return res, err
	}
	want := render(next)
	cur, err := os.ReadFile(own)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, err
	}
	if !bytes.Equal(cur, want) {
		if err := writeFile(own, want); err != nil {
			return res, err
		}
		res.Changed = true
	}
	inc, err := ensureInclude(home)
	if err != nil {
		return res, err
	}
	res.Include, res.Created, res.Backup = inc.Include, inc.Created, inc.Backup
	res.Changed = res.Changed || inc.Changed
	return res, nil
}

// Remove drops the blocks for rooms from ~/.ssh/atrium_config, or every block when rooms is empty. It never touches
// ~/.ssh/config, and it never removes the Include line. It returns the rooms that were not there.
func Remove(home string, rooms []string) (removed, missing []string, err error) {
	have, err := readOwn(home)
	if err != nil {
		return nil, nil, err
	}
	var keep []block
	if len(rooms) == 0 {
		for _, b := range have {
			removed = append(removed, b.room)
		}
	} else {
		drop := map[string]bool{}
		for _, r := range rooms {
			drop[strings.ToLower(r)] = true
		}
		seen := map[string]bool{}
		for _, b := range have {
			if drop[strings.ToLower(b.room)] {
				removed = append(removed, b.room)
				seen[strings.ToLower(b.room)] = true
				continue
			}
			keep = append(keep, b)
		}
		for _, r := range rooms {
			if !seen[strings.ToLower(r)] {
				missing = append(missing, r)
			}
		}
	}
	if len(removed) == 0 {
		return nil, missing, nil
	}
	_, _, own := Paths(home)
	return removed, missing, writeFile(own, render(keep))
}

// Show is what is configured: the rooms with a block, the file's text, and whether the user's config includes it.
type Shown struct {
	Rooms    []string
	Text     string
	Included bool
	// ConfigExists is false when ~/.ssh/config is not there at all.
	ConfigExists bool
}

// Show reads, and changes nothing.
func Show(home string) (Shown, error) {
	var s Shown
	_, cfg, own := Paths(home)
	b, err := os.ReadFile(own)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return s, err
	}
	s.Text = string(b)
	for _, bl := range parse(b) {
		s.Rooms = append(s.Rooms, bl.room)
	}
	c, err := os.ReadFile(cfg)
	if err == nil {
		s.ConfigExists = true
		s.Included = hasInclude(c)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return s, err
	}
	return s, nil
}

// hasInclude is whether a line of the config is an Include that names atrium_config.
func hasInclude(b []byte) bool {
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) < 2 || !strings.EqualFold(f[0], "include") {
			continue
		}
		for _, p := range f[1:] {
			if strings.Trim(p, `"`) == File {
				return true
			}
		}
	}
	return false
}

// ensureInclude puts the one line at the TOP of ~/.ssh/config if it is missing, after a timestamped backup. A config
// that already has it is not touched and not backed up. A missing config is created with just that line.
//
// THE TOP, because ssh reads an Include where it stands: before any Host or Match line it applies to every host, and
// after one it would be inside that block.
func ensureInclude(home string) (Result, error) {
	var res Result
	_, cfg, _ := Paths(home)
	cur, err := os.ReadFile(cfg)
	if errors.Is(err, fs.ErrNotExist) {
		if err := writeFile(cfg, []byte(IncludeLine+"\n")); err != nil {
			return res, err
		}
		return Result{Changed: true, Include: true, Created: true}, nil
	}
	if err != nil {
		return res, err
	}
	if hasInclude(cur) {
		return res, nil
	}
	backup := cfg + ".atrium-backup-" + time.Now().Format("20060102T150405")
	for i := 1; ; i++ {
		if _, err := os.Stat(backup); errors.Is(err, fs.ErrNotExist) {
			break
		}
		backup = fmt.Sprintf("%s.atrium-backup-%s-%d", cfg, time.Now().Format("20060102T150405"), i)
	}
	if err := os.WriteFile(backup, cur, 0o600); err != nil {
		return res, fmt.Errorf("could not back up %s first: %w", cfg, err)
	}
	nl := "\n"
	if i := bytes.IndexByte(cur, '\n'); i > 0 && cur[i-1] == '\r' {
		nl = "\r\n"
	}
	next := append([]byte(IncludeLine+nl), cur...)
	// A config that is a symlink keeps being one: the new text goes to what it points at.
	target := cfg
	if r, err := filepath.EvalSymlinks(cfg); err == nil {
		target = r
	}
	if err := writeFile(target, next); err != nil {
		return res, err
	}
	return Result{Changed: true, Include: true, Backup: backup}, nil
}

// writeFile replaces a file through a temp file and a rename, so a reader never sees half of it. Mode 0600, which is
// what ssh wants of its config.
func writeFile(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	_ = os.Chmod(name, 0o600)
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
