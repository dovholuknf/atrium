package link

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/edge"
)

// An UNSCOPED launch finds its own room: the one on the caller's machine that has
// the directory.
//
// `gwt claude .` posts a launch with the directory it is standing in and no room.
// With more than one room attached that was a 409 asking which, and the question
// had a wrong answer on offer: a room on another machine, where the directory is
// not, so the retry failed with "is not a directory" and gwt fell back to a plain
// tab. The hub can know better. A launch means "here", and here is a machine.
//
// WHAT THIS DOES NOT TOUCH: a launch that names a room, by header, by tag or by a
// card, is placed by `roomFor` and `placeCard` exactly as before, and this never
// sees it. Another unscoped write still gets `needsARoom`. There is no default
// room and no setting: somebody who wants a room names it.

// launchProbeWait is how long a room has to say whether it has the directory. A
// room that does not answer in this time is not a match, so one silent machine
// cannot hold up a launch that another room can take.
const launchProbeWait = 4 * time.Second

// The seams. A test replaces these.
var (
	// hubHostname is the machine the hub runs on, spelled the way a room spells its
	// own `Host`: both come from os.Hostname.
	hubHostname = os.Hostname
	// launchProbeFor is `launchProbeWait`, or a test's shorter one.
	launchProbeFor = launchProbeWait
)

// placeLaunch picks the room for an unscoped `POST /v1/launch`, or answers it.
//
// Returns the request to carry on with and whether to carry on. It carries on
// either untouched, when this does not apply, or with the room set the way a
// caller naming it would have set it, so everything after (the deletion gate,
// the dial, the per-room caps) sees an ordinary launch that named its room.
func (p *Proxy) placeLaunch(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/launch" || r.Body == nil {
		return r, true
	}
	// Named already, by header, tag, card or because only one room is attached.
	if room, _ := p.roomFor(r); room != "" {
		return r, true
	}
	rooms := p.hub.Rooms()
	if len(rooms) < 2 {
		return r, true
	}
	// No directory in the body is no question to ask: the room would use the
	// runner's own. That stays what it was, a request for a room.
	cwd := launchCwdIn(r)
	if cwd == "" {
		return r, true
	}

	// A DIRECTORY THE HUB CANNOT JUDGE IS NOT PROBED: a relative one ("." or
	// "~/x" means nothing off the machine it was typed on) and a UNC one, which
	// is worse. See `launchCwdKind`.
	if launchCwdKind(cwd) != cwdAbsolute {
		needsARoom(w, rooms)
		return r, false
	}

	cands := launchCandidates(r, p.leavingOut(rooms))
	// NOTHING TO ASK, as when every room is on its way out: the old question.
	if len(cands) == 0 {
		needsARoom(w, rooms)
		return r, false
	}
	has, quiet := p.roomsWithDir(r.Context(), cands, cwd)
	switch len(has) {
	case 1:
		r = r.Clone(r.Context())
		r.Header.Set(RoomHeader, has[0])
		return r, true
	case 0:
		// A ROOM THAT DID NOT ANSWER MAY HAVE THE DIRECTORY, so "none matched" is
		// not known. It is a room on a build from before the check, one that is
		// slow, or one that is restarting, and the picker worked for all of them
		// before this existed: the human could pick it. Killing the picker would
		// strand every launch whose directory is on a room not yet updated. So the
		// OLD question comes back, over EVERY attached room and not only the
		// candidates, which also makes the order hub and rooms are deployed in
		// irrelevant. Only the 422 below is new.
		if len(quiet) > 0 {
			needsARoom(w, rooms)
			return r, false
		}
		// EVERY CANDIDATE ANSWERED AND NONE HAS IT. NOT A 409 WITH `rooms`: a caller
		// shows its picker on exactly that, and a pick from a list where nothing
		// has the directory can only fail.
		cardAnswer(w, http.StatusUnprocessableEntity, map[string]any{
			"error": "no room has the directory " + cwd + ". looked on " + strings.Join(attachedNames(cands), ", "),
		})
	default:
		// Only the rooms that have it. sg4 runs two rooms on one machine, so this is
		// a real answer there, and the caller picks between two that will work.
		cardAnswer(w, http.StatusConflict, map[string]any{
			"error": cwd + " is on more than one room. pick one: " + strings.Join(has, ", "),
			"rooms": has,
		})
	}
	return r, false
}

const (
	cwdAbsolute = iota
	cwdRelative
	cwdUNC
)

// launchCwdKind says what a directory is as TEXT, without looking at any
// filesystem: the hub may be Windows or not and the caller's path may be either
// style, so `filepath.IsAbs` of the hub's own OS answers the wrong question.
// Absolute is "/x", "C:\x" or "C:/x".
//
// A UNC PATH IS NEVER PROBED. `\\host\share`, `//host/share`, `\\?\UNC\...` and
// `\\.\...` (any two leading slashes of either kind, which Windows reads as one)
// make `os.Stat` on a Windows room open an SMB connection to that host with the
// room user's credentials, so a probe fanned out to every candidate would send
// the user's NTLM hash to whatever host the caller wrote. The room refuses them
// too (internal/api/launchcwd.go) so a direct call cannot do it either.
func launchCwdKind(cwd string) int {
	slash := func(c byte) bool { return c == '/' || c == '\\' }
	if len(cwd) >= 2 && slash(cwd[0]) && slash(cwd[1]) {
		return cwdUNC
	}
	if len(cwd) >= 1 && cwd[0] == '/' {
		return cwdAbsolute
	}
	if len(cwd) >= 3 && cwd[1] == ':' && slash(cwd[2]) &&
		(cwd[0] >= 'a' && cwd[0] <= 'z' || cwd[0] >= 'A' && cwd[0] <= 'Z') {
		return cwdAbsolute
	}
	return cwdRelative
}

// leavingOut drops the rooms on their way out. A room marked for deletion starts
// nothing new (`startsNothing`, which reads the same inventory), so asking it
// whether it has the directory would only find a room the launch is then refused
// on. A hub with no inventory has no marks.
func (p *Proxy) leavingOut(rooms []Attached) []Attached {
	stock := p.inventory()
	if stock == nil {
		return rooms
	}
	known, err := stock.Known()
	if err != nil {
		return rooms
	}
	var out []Attached
	for _, a := range rooms {
		leaving := false
		for _, k := range known {
			if equalFold(k.Name, a.Name) && k.State == "marked-for-deletion" {
				leaving = true
			}
		}
		if !leaving {
			out = append(out, a)
		}
	}
	return out
}

// launchCandidates are the rooms worth asking: those on the caller's machine.
//
// A LOCAL CALLER IS ON THE HUB'S MACHINE. `edge.LocalOperator` is how the hub
// tells, the same test every "this machine" gate here uses, so a caller on
// loopback has the hub's own hostname and the rooms whose `Host` is that one.
// That is what `gwt` on sg4 is, and it keeps claude-sg4 and sg4-control in the
// running while leaving sg3 and m1mini out.
//
// A CALLER THE HUB CANNOT PLACE gets every room. A request that arrived over the
// network says nothing about which machine it was typed on, and guessing from an
// address would be worse than asking more rooms: the directory check below still
// decides, so this only costs a few more probes.
//
// A host filter that matches no room is no filter, for the same reason. Rooms
// that never reported a host, or spell it otherwise, would otherwise make every
// launch fail on a machine that has the directory.
func launchCandidates(r *http.Request, rooms []Attached) []Attached {
	if !edge.LocalOperator(r) {
		return rooms
	}
	here, err := hubHostname()
	if err != nil || strings.TrimSpace(here) == "" {
		return rooms
	}
	var same []Attached
	for _, a := range rooms {
		if a.Host != "" && equalFold(a.Host, here) {
			same = append(same, a)
		}
	}
	if len(same) == 0 {
		return rooms
	}
	return same
}

// roomsWithDir asks each room, at once, whether it has the directory. Returns the
// rooms that do, sorted, and the ones that did not answer, sorted.
func (p *Proxy) roomsWithDir(ctx context.Context, rooms []Attached, dir string) (has, quiet []string) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, a := range rooms {
		wg.Add(1)
		go func(room string) {
			defer wg.Done()
			isDir, answered := p.roomHasDir(ctx, room, dir)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case !answered:
				quiet = append(quiet, room)
			case isDir:
				has = append(has, room)
			}
		}(a.Name)
	}
	wg.Wait()
	sort.Strings(has)
	sort.Strings(quiet)
	return has, quiet
}

// roomHasDir asks one room. `answered` is false for anything but a clean answer:
// a room that is slow, gone, or too old to have the route.
func (p *Proxy) roomHasDir(ctx context.Context, room, dir string) (isDir, answered bool) {
	ctx, cancel := context.WithTimeout(ctx, launchProbeFor)
	defer cancel()
	u := "http://" + hostFor(room) + "/v1/launch/cwd?path=" + url.QueryEscape(dir)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, false
	}
	res, err := p.roomClient(room).Do(req)
	if err != nil {
		return false, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
		return false, false
	}
	var body struct {
		Dir bool `json:"dir"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&body) != nil {
		return false, false
	}
	return body.Dir, true
}

// launchCwdIn reads the `cwd` off a launch and leaves the body readable again
// for the room. A body over the room's own limit is put back whole and says
// nothing, so the room refuses it as it always did.
func launchCwdIn(r *http.Request) string {
	raw, err := io.ReadAll(io.LimitReader(r.Body, launchBodyLimit+1))
	if err != nil || len(raw) > launchBodyLimit {
		r.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(raw), r.Body), r.Body}
		return ""
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
	var body struct {
		Cwd string `json:"cwd"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	return strings.TrimSpace(body.Cwd)
}

func attachedNames(rooms []Attached) []string {
	names := make([]string, 0, len(rooms))
	for _, a := range rooms {
		names = append(names, a.Name)
	}
	sort.Strings(names)
	return names
}
