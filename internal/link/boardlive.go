package link

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

// liveBoard is the version of a board served from disk, which changes under a running hub. The hub used to hash it once
// at start, so an open board was told nothing of a merge until the hub restarted.
type liveBoard struct {
	mu    sync.Mutex
	at    time.Time
	stamp string
	now   boardVersion
	hash  func(fs.FS) string
	parts func(fs.FS) (css, rest string)
}

// boardVersion is what the `board` event carries: the whole build, and its stylesheets apart from everything else.
type boardVersion struct {
	Build string `json:"build"`
	CSS   string `json:"css"`
	Rest  string `json:"rest"`
}

// SetLiveBoard makes the root board's version follow the files on disk.
func (p *Proxy) SetLiveBoard(hash func(fs.FS) string, parts func(fs.FS) (css, rest string)) {
	p.live = &liveBoard{hash: hash, parts: parts}
}

// get is the version now. A listing of names, sizes and times is the cheap look, and the files are hashed only when
// that changed.
func (l *liveBoard) get(fsys fs.FS) boardVersion {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.at.IsZero() && time.Since(l.at) < viewTTL {
		return l.now
	}
	l.at = time.Now()
	if s := statStamp(fsys); s != l.stamp || l.now.Build == "" {
		l.stamp = s
		l.now.Build = l.hash(fsys)
		l.now.CSS, l.now.Rest = l.parts(fsys)
	}
	return l.now
}

func statStamp(fsys fs.FS) string {
	h := ""
	_ = fs.WalkDir(fsys, ".", func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		if info, err := e.Info(); err == nil {
			h += fmt.Sprintf("%s|%d|%d\n", path, info.Size(), info.ModTime().UnixNano())
		}
		return nil
	})
	return h
}

// watchBoard says `board` to every watching client when the board's files change, and once to each client that
// arrives so it learns what it is looking at. Ticks only while somebody watches, and costs one listing per tick however
// many cards there are.
func (f *feeds) watchBoard(ctx context.Context) {
	if f.p.live == nil || f.p.board == nil {
		return
	}
	tick := time.NewTicker(viewTTL)
	defer tick.Stop()
	var last boardVersion
	for {
		if v := f.p.live.get(f.p.board); v != last {
			last = v
			if data, err := json.Marshal(v); err == nil {
				f.broadcast(wire.Event{Kind: "board", Data: data})
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// openBoard is the first `board` event of a stream.
func (p *Proxy) openBoard(ch chan<- wire.Event) {
	if p.live == nil || p.board == nil {
		return
	}
	if data, err := json.Marshal(p.live.get(p.board)); err == nil {
		select {
		case ch <- wire.Event{Kind: "board", Data: data}:
		default:
		}
	}
}
