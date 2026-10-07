package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/cardproc"
	"github.com/dovholuknf/atrium/internal/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// A CARD'S PROCESSES AND PORTS, AS INVENTORY. Design: docs/rnd/card-lifecycle-design.md section 9. Item
// r-card-procs-ports. The process registry (a process atrium runs in a pty) is its own item: this is the half that
// lets a card own what its agent started, so closing the card stops it.
//
//	POST /v1/tasks/{id}/ports {count}       free ports from the room's card range, recorded on the card
//	POST /v1/tasks/{id}/own   {kind, ref}   record a proc (a pid), a dir or a port the card's agent made
//
// A PORT IS HANDED OUT ONCE. It is in the room's card range, outside every range Windows reserved, held by no live
// row of any card here, and a loopback bind on it succeeds. The range is the setting ports.card_range, else a block
// of 200 picked from the room's name, so two rooms on one machine land in different blocks. Previews keep
// 50000-50999, so no block is there.
//
// A PROC IS A PID AND ITS START TIME, read when it is recorded. Close stops the tree only while the start time still
// matches, so a pid the system gave to something else is never stopped. The sweep marks one gone or recycled freed.

// SettingCardPorts is the room's card port range, "lo-hi".
const SettingCardPorts = "ports.card_range"

// The default blocks: 200 ports each from 20000, below the 49152 dynamic range and clear of the previews.
const (
	cardPortBase   = 20000
	cardPortBlock  = 200
	cardPortBlocks = 140
	maxPortsAsked  = 20
)

// portMu serialises handing out, so two asks never get the same port.
var portMu sync.Mutex

// excludedCache holds the reserved ranges for a minute: Hyper-V and WSL move them at boot, and netsh is slow.
var excludedCache struct {
	sync.Mutex
	at time.Time
	rs []cardproc.Range
}

// excludedPorts is the seam a test replaces.
var excludedPorts = func() []cardproc.Range {
	excludedCache.Lock()
	defer excludedCache.Unlock()
	if time.Since(excludedCache.at) > time.Minute {
		excludedCache.rs, excludedCache.at = cardproc.Excluded(), time.Now()
	}
	return excludedCache.rs
}

// cardPortRange is the room's range: the setting, else the block its name hashes to.
func (s *Server) cardPortRange() (cardproc.Range, error) {
	if v, _ := s.st.Setting(SettingCardPorts); strings.TrimSpace(v) != "" {
		return cardproc.ParseRange(v)
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(s.Room))))
	lo := cardPortBase + int(h.Sum32()%cardPortBlocks)*cardPortBlock
	return cardproc.Range{Lo: lo, Hi: lo + cardPortBlock - 1}, nil
}

// heldPorts is every port a live row names, on any card of this room.
func (s *Server) heldPorts() map[int]bool {
	out := map[int]bool{}
	rows, _ := s.st.LiveResources()
	for _, r := range rows {
		if r.Kind == store.ResPort {
			if n, err := strconv.Atoi(r.Ref); err == nil {
				out[n] = true
			}
		}
	}
	return out
}

// bindsFree is whether a loopback listen on p works now.
func bindsFree(p int) bool {
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// POST /v1/tasks/{id}/ports
func (s *Server) postCardPorts(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var in struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if in.Count == 0 {
		in.Count = 1
	}
	if in.Count < 0 || in.Count > maxPortsAsked {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("ask for 1 to %d ports", maxPortsAsked))
		return
	}
	rng, err := s.cardPortRange()
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("%s: %w", SettingCardPorts, err))
		return
	}
	portMu.Lock()
	defer portMu.Unlock()
	held, reserved := s.heldPorts(), excludedPorts()
	var got []int
	for p := rng.Lo; p <= rng.Hi && len(got) < in.Count; p++ {
		if held[p] || inRanges(reserved, p) || !bindsFree(p) {
			continue
		}
		if _, err := s.st.AddResource(t.ID, store.ResPort, strconv.Itoa(p), ""); err != nil {
			s.fail(w, err)
			return
		}
		got = append(got, p)
	}
	if len(got) < in.Count {
		writeJSON(w, http.StatusConflict, map[string]any{"code": "no_ports", "ports": orInts(got), "range": rng.String(),
			"error": fmt.Sprintf("only %d free ports are left in %s. close cards that hold some, or widen %s",
				len(got), rng, SettingCardPorts)})
		s.publishTaskID(t.ID)
		return
	}
	s.publishTaskID(t.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ports": got, "range": rng.String()})
}

func inRanges(rs []cardproc.Range, p int) bool {
	for _, r := range rs {
		if r.Has(p) {
			return true
		}
	}
	return false
}

func orInts(v []int) []int {
	if v == nil {
		return []int{}
	}
	return v
}

func (s *Server) publishTaskID(id string) {
	if t, err := s.st.Get(id); err == nil {
		s.PublishTask(t)
	}
}

// POST /v1/tasks/{id}/own
func (s *Server) postOwn(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var in struct {
		Kind string `json:"kind"`
		Ref  string `json:"ref"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ref, detail := strings.TrimSpace(in.Ref), ""
	switch strings.TrimSpace(in.Kind) {
	case store.ResProc:
		pid, err := strconv.Atoi(ref)
		if err != nil || pid <= 0 {
			writeErr(w, http.StatusBadRequest, errors.New("a proc's ref is its pid"))
			return
		}
		if pid == os.Getpid() {
			writeErr(w, http.StatusBadRequest, errors.New("that pid is atrium itself"))
			return
		}
		if detail, err = cardproc.StartTime(pid); err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("pid %d: %v", pid, err))
			return
		}
		ref = strconv.Itoa(pid)
	case store.ResPort:
		p, err := strconv.Atoi(ref)
		if err != nil || p < 1 || p > 65535 {
			writeErr(w, http.StatusBadRequest, errors.New("a port's ref is its number"))
			return
		}
		ref = strconv.Itoa(p)
	case store.ResDir:
		dir, err := s.ownableDir(t, ref)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		ref = dir
	default:
		writeErr(w, http.StatusBadRequest, errors.New("kind is proc, port or dir"))
		return
	}
	row, err := s.st.AddResource(t.ID, in.Kind, ref, detail)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.publishTaskID(t.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"resource": row})
}

// ownableDir is a folder a card may own: an existing directory inside its own worktree, or under the scratch root, and
// never the worktree itself.
func (s *Server) ownableDir(t *store.Task, want string) (string, error) {
	if want == "" || !filepath.IsAbs(filepath.FromSlash(want)) {
		return "", errors.New("a dir's ref is an absolute path")
	}
	for _, root := range []string{t.Worktree, s.scratchRoot()} {
		if strings.TrimSpace(root) == "" {
			continue
		}
		real, err := safepath.Contained(filepath.FromSlash(root), filepath.FromSlash(want))
		if err != nil || eqPath(real, filepath.FromSlash(root)) {
			continue
		}
		if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
			return "", errors.New(want + " is not a directory")
		}
		return filepath.ToSlash(real), nil
	}
	return "", errors.New("a card owns only folders inside its own worktree or the scratch folder, and not the worktree itself")
}
