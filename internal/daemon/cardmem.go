package daemon

import (
	"context"
	"sync"
	"time"
)

// cardMemEvery is how often the pty host is asked what each card's processes hold.
const cardMemEvery = 15 * time.Second

// cardMem is each card's memory in bytes: its runner's and its shell's job object, cgroup or process group, as the
// pty host measures them. Empty with pty_host off, since only the host holds the jobs.
type cardMem struct {
	mu sync.Mutex
	by map[string]int64
}

func (d *Daemon) cardMemLoop(ctx context.Context) {
	t := time.NewTicker(cardMemEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.sampleCardMem()
		}
	}
}

// sampleCardMem reads the host's list and republishes a card whose memory moved enough to show.
func (d *Daemon) sampleCardMem() {
	by := map[string]int64{}
	if d.st.PtyHostOn() {
		if cl, _, err := d.hostClient(false, false); err == nil {
			if list, err := cl.List(); err == nil {
				for _, p := range list {
					if !p.Exited {
						by[p.ID] += p.Mem
					}
				}
			}
		}
	}
	d.cmem.mu.Lock()
	old := d.cmem.by
	d.cmem.by = by
	d.cmem.mu.Unlock()
	for id, n := range by {
		if memMoved(old[id], n) {
			d.Publish(id)
		}
	}
	for id := range old {
		if _, ok := by[id]; !ok {
			d.Publish(id)
		}
	}
}

// memMoved is a change of more than a tenth, or 16 MB, so a card is not republished for noise.
func memMoved(was, now int64) bool {
	diff := now - was
	if diff < 0 {
		diff = -diff
	}
	return diff > max(was/10, 16<<20)
}

func (d *Daemon) memFor(taskID string) int64 {
	d.cmem.mu.Lock()
	defer d.cmem.mu.Unlock()
	return d.cmem.by[taskID]
}
