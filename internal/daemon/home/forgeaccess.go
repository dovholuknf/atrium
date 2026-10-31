package home

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/forge"
	"github.com/dovholuknf/atrium/internal/store"
)

// A forge CLI (gh, bb, glab) that is missing, logged out or short of a scope.
//
// clint's rule: when forge access is NECESSARY, atrium raises an alert, says what to run in a message, and offers the
// configuration. Forges are used only on ask, so nothing here runs ahead of a need and nothing polls. Two things ask:
// a preflight naming a forge, and RaiseForgeAccess, which the code that hits the missing access calls.
//
// The check is the CLI's own status command, never a token read. It runs on a room with no hub only: a room with a hub
// never runs a forge CLI, and the hub keeps the logins, the check and the alert (internal/link/forgeroute.go).

const (
	ForgeOK           = "ok"
	ForgeNotInstalled = "not_installed"
	ForgeLoggedOut    = "logged_out"
	ForgeMissingScope = "missing_scope"
	ForgeUnknown      = "unknown"
)

// forgeStatusArgs is the fixed status command per tool.
var ForgeStatusArgs = map[string]func(host string) []string{
	"gh":   func(h string) []string { return []string{"auth", "status", "--hostname", h} },
	"glab": func(h string) []string { return []string{"auth", "status", "--hostname", h} },
	"bb":   func(h string) []string { return []string{"auth", "status"} },
}

type ForgeStatus struct {
	Tool    string   `json:"tool"`
	Host    string   `json:"host"`
	State   string   `json:"state"`
	Missing []string `json:"missing,omitempty"`
	Message string   `json:"message,omitempty"`
	Fix     string   `json:"fix,omitempty"`
	Path    string   `json:"path,omitempty"`
}

var ScopesLineRE = regexp.MustCompile(`(?i)token scopes?:\s*(.*)`)

// forgeScopesIn reads the scopes a status output says the login holds. ok is false when it says nothing about them,
// as a fine-grained token's does, in which case nothing can be said to be missing.
func ForgeScopesIn(out string) (have map[string]bool, ok bool) {
	m := ScopesLineRE.FindStringSubmatch(out)
	if m == nil {
		return nil, false
	}
	have = map[string]bool{}
	for _, f := range strings.FieldsFunc(m[1], func(r rune) bool {
		return r == ',' || r == ' ' || r == '\'' || r == '"' || r == '\r'
	}) {
		have[f] = true
	}
	return have, true
}

func (d *Service) RoomName() string {
	if r := strings.TrimSpace(d.Room()); r != "" {
		return r
	}
	return "this room"
}

// forgeSay is the sentence for a state and the one command that fixes it.
func (d *Service) ForgeSay(tool, cmd, host, state string, missing []string) (message, fix string) {
	room := d.RoomName()
	login := cmd + " auth login"
	if tool != "bb" {
		login += " --hostname " + host
	}
	switch state {
	case ForgeNotInstalled:
		return fmt.Sprintf("%s is not installed on %s: install it on %s, then run `%s`", cmd, room, room, login), login
	case ForgeMissingScope:
		fix = login
		if tool == "gh" {
			fix = fmt.Sprintf("%s auth refresh --hostname %s --scopes %s", cmd, host, strings.Join(missing, ","))
		}
		return fmt.Sprintf("%s on %s is missing the scope %s for %s: run `%s` on %s",
			cmd, room, strings.Join(missing, ", "), host, fix, room), fix
	}
	return fmt.Sprintf("%s is not logged in on %s: run `%s` on %s", cmd, room, login, room), login
}

// checkForge asks one forge CLI whether this room is logged in to host. scopes may be nil.
func (d *Service) CheckForge(ctx context.Context, tool, host string, scopes []string) ForgeStatus {
	st := ForgeStatus{Tool: tool, Host: host}
	argsFor, known := ForgeStatusArgs[tool]
	if !known || !store.KnownForge(tool) {
		st.State, st.Message = ForgeUnknown, "unknown forge CLI, nothing was run"
		return st
	}
	if host == "" {
		host = store.ForgeDefaultHost(tool)
		st.Host = host
	}
	cmd := tool
	path, err := d.PreflightLook(cmd)
	if err != nil {
		st.State = ForgeNotInstalled
		st.Message, st.Fix = d.ForgeSay(tool, cmd, host, st.State, nil)
		return st
	}
	st.Path = path
	cctx, cancel := context.WithTimeout(ctx, d.PreflightEachFor())
	defer cancel()
	var buf bytes.Buffer
	err = d.PreflightRun(cctx, path, argsFor(host), &shared.LimitedWriter{W: &buf, Left: shared.PreflightOutput})
	switch {
	case cctx.Err() != nil:
		st.State, st.Message = ForgeUnknown, cmd+" did not answer in time"
		return st
	case err != nil:
		st.State = ForgeLoggedOut
		st.Message, st.Fix = d.ForgeSay(tool, cmd, host, st.State, nil)
		return st
	}
	if have, ok := ForgeScopesIn(buf.String()); ok {
		for _, w := range scopes {
			if !have[w] {
				st.Missing = append(st.Missing, w)
			}
		}
	}
	if len(st.Missing) > 0 {
		st.State = ForgeMissingScope
		st.Message, st.Fix = d.ForgeSay(tool, cmd, host, st.State, st.Missing)
		return st
	}
	st.State = ForgeOK
	return st
}

// ForgeAlert is one open alert: a forge this room needs and cannot use.
type ForgeAlert struct {
	Key     string `json:"key"`
	Room    string `json:"room"`
	Tool    string `json:"tool"`
	Host    string `json:"host"`
	State   string `json:"state"`
	Message string `json:"message"`
	Fix     string `json:"fix"`
	At      string `json:"at"`
}

// applyForge records a checked status: a failure raises the alert, ok ends it.
func (d *Service) ApplyForge(st ForgeStatus) {
	if st.State == ForgeUnknown {
		return
	}
	key := st.Tool + "@" + st.Host
	d.ForgeMu.Lock()
	if d.ForgeOpen == nil {
		d.ForgeOpen = map[string]ForgeAlert{}
	}
	old, was := d.ForgeOpen[key]
	if st.State == ForgeOK {
		delete(d.ForgeOpen, key)
		d.ForgeMu.Unlock()
		if was {
			d.Ap.Broadcast("forge-access", map[string]any{"key": key, "room": d.RoomName(), "cleared": true})
		}
		return
	}
	a := ForgeAlert{Key: key, Room: d.RoomName(), Tool: st.Tool, Host: st.Host, State: st.State,
		Message: st.Message, Fix: st.Fix, At: time.Now().UTC().Format(time.RFC3339)}
	same := was && old.State == a.State && old.Message == a.Message
	if same {
		a.At = old.At
	}
	d.ForgeOpen[key] = a
	d.ForgeMu.Unlock()
	// A repeat of what is already open is not said again, or one failing action would toast on every retry.
	if !same {
		d.Ap.Broadcast("forge-access", a)
	}
}

// RaiseForgeAccess is the hook for a runtime failure: the forge package's AccessError{Tool, Host, Detail} goes
// through here, as `d.RaiseForgeAccess(e.Tool, e.Host, e.Detail)`. It raises the same alert and message a failed
// check does. Nothing is run, since the caller already knows the access is missing. The state is logged_out unless
// the detail says the CLI was not found.
func (d *Service) RaiseForgeAccess(tool, host, detail string) {
	if !store.KnownForge(tool) {
		return
	}
	if host == "" {
		host = store.ForgeDefaultHost(tool)
	}
	cmd := tool
	state := ForgeLoggedOut
	low := strings.ToLower(detail)
	for _, w := range []string{"not found", "executable file", "not installed", "no such file"} {
		if strings.Contains(low, w) {
			state = ForgeNotInstalled
		}
	}
	st := ForgeStatus{Tool: tool, Host: host, State: state}
	st.Message, st.Fix = d.ForgeSay(tool, cmd, host, state, nil)
	d.ApplyForge(st)
}

// forgeToolOfKind is the CLI a forge kind runs through.
var ForgeToolOfKind = map[string]string{"github": "gh", "bitbucket": "bb", "gitlab": "glab"}

// ForgeAccessFrom raises the alert when err is, or wraps, a forge AccessError. kind is the forge kind that failed, so a
// wrapper command named by the provider still raises the alert of its tool. It reports whether err was one.
func (d *Service) ForgeAccessFrom(kind string, err error) bool {
	var ae *forge.AccessError
	if !errors.As(err, &ae) {
		return false
	}
	tool := ForgeToolOfKind[kind]
	if tool == "" {
		tool = ae.Tool
	}
	d.RaiseForgeAccess(tool, ae.Host, ae.Detail)
	return true
}

// ForgeWorked clears the alert of a forge that just answered. kind is the forge kind, as forge.Forge.Kind says it.
// Nothing is run, and no alert open means nothing is said.
func (d *Service) ForgeWorked(kind, host string) {
	tool := ForgeToolOfKind[kind]
	if tool == "" {
		return
	}
	if host == "" {
		host = store.ForgeDefaultHost(tool)
	}
	d.ForgeMu.Lock()
	_, open := d.ForgeOpen[tool+"@"+host]
	d.ForgeMu.Unlock()
	if open {
		d.ApplyForge(ForgeStatus{Tool: tool, Host: host, State: ForgeOK})
	}
}

// ForgeAlerts is the open set, for the settings view.
func (d *Service) ForgeAlerts() any {
	d.ForgeMu.Lock()
	defer d.ForgeMu.Unlock()
	out := make([]ForgeAlert, 0, len(d.ForgeOpen))
	for _, a := range d.ForgeOpen {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
