package store

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// fileSink is a cold, write-only sink: it appends one JSON line per event to
// files under a directory, rolling to a new file by day and by size. An
// operator moves a closed file off the machine and deletes it; nothing here
// reads them back, so Recent is unsupported.
//
// It is best effort, which the daemon's resilience rules make non-negotiable: a
// hook must never fail a session, so a slow or full disk cannot be allowed to
// block Append. Events are handed to a channel and written on the sink's own
// goroutine. When the channel is full the event is DROPPED and a loss counter
// ticks, rather than the caller stalling behind the disk.
type fileSink struct {
	dir      string
	maxBytes int64

	ch   chan *Event
	done chan struct{}
	wg   sync.WaitGroup

	// dropped counts events lost to backpressure or a write error. Read from
	// any goroutine, so atomic.
	dropped atomic.Uint64

	// Owned by the writer goroutine alone, so no lock: the current file, how
	// many bytes it holds, the day it belongs to, and a per-process counter that
	// keeps two segments opened in the same second from colliding.
	f    *os.File
	size int64
	day  string
	seq  int
}

const (
	// fileSinkQueue is how many events may wait for the writer before new ones
	// are dropped. Deep enough to ride out a disk hiccup, bounded so a wedged
	// disk cannot grow memory without limit.
	fileSinkQueue = 4096
	// fileSinkMaxBytes rolls a segment once it passes this size, so an operator
	// moving closed files off the machine deals in chunks rather than one file
	// that only grows.
	fileSinkMaxBytes = 8 << 20 // 8 MiB
)

// newFileSink creates the directory and starts the writer goroutine with the
// default roll size.
func newFileSink(dir string) (*fileSink, error) {
	return newFileSinkWith(dir, fileSinkMaxBytes)
}

// newFileSinkWith is newFileSink with an explicit roll size, so a test can force
// rolling without writing megabytes. maxBytes is set before the writer starts,
// so the goroutine never races the caller for it.
func newFileSinkWith(dir string, maxBytes int64) (*fileSink, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("event file sink %s: %w", dir, err)
	}
	fs := &fileSink{
		dir:      dir,
		maxBytes: maxBytes,
		ch:       make(chan *Event, fileSinkQueue),
		done:     make(chan struct{}),
	}
	fs.wg.Add(1)
	go fs.loop()
	return fs, nil
}

// Append enqueues an event for the writer. It never blocks and never fails: a
// full queue drops the event and counts the loss.
func (fs *fileSink) Append(taskID string, e *Event) error {
	select {
	case fs.ch <- e:
	default:
		// The queue is full, which means the disk is not keeping up. Drop rather
		// than stall the caller, which is a hook that must not fail a session.
		// The first loss in a run is logged; the rest are counted and reported
		// on Close, so a wedged disk does not itself become a log flood.
		if fs.dropped.Add(1) == 1 {
			log.Printf("event file sink: queue full, dropping events (dir %s)", fs.dir)
		}
	}
	return nil
}

// Recent is unsupported: this is a write-only cold sink.
func (fs *fileSink) Recent(taskID string, limit int) ([]*Event, error) {
	return nil, ErrRecentUnsupported
}

// Close stops the writer after draining what is already queued, flushes the
// current file, and reports any losses.
func (fs *fileSink) Close() error {
	close(fs.done)
	fs.wg.Wait()
	if d := fs.dropped.Load(); d > 0 {
		log.Printf("event file sink: dropped %d event(s) under backpressure (dir %s)", d, fs.dir)
	}
	return nil
}

// loop is the single writer. It drains the queue on shutdown so events already
// accepted are not lost to a clean stop, then closes the open file.
func (fs *fileSink) loop() {
	defer fs.wg.Done()
	for {
		select {
		case e := <-fs.ch:
			fs.write(e)
		case <-fs.done:
			for {
				select {
				case e := <-fs.ch:
					fs.write(e)
				default:
					fs.closeFile()
					return
				}
			}
		}
	}
}

// write appends one event as a JSON line, rolling first if the day changed or
// the segment is full. A write or roll failure counts a loss rather than
// propagating: this sink's contract is best effort.
func (fs *fileSink) write(e *Event) {
	line, err := json.Marshal(e)
	if err != nil {
		fs.dropped.Add(1)
		return
	}
	line = append(line, '\n')
	day := e.At.UTC().Format("2006-01-02")
	if fs.f == nil || day != fs.day || fs.size+int64(len(line)) > fs.maxBytes {
		if err := fs.roll(e.At); err != nil {
			if fs.dropped.Add(1) == 1 {
				log.Printf("event file sink: roll failed: %v", err)
			}
			return
		}
	}
	n, err := fs.f.Write(line)
	fs.size += int64(n)
	if err != nil {
		if fs.dropped.Add(1) == 1 {
			log.Printf("event file sink: write failed: %v", err)
		}
	}
}

// roll closes the current segment and opens a new one named for the time it
// opened. The day is in the name so an operator can sort and retire whole days,
// and a per-process sequence keeps two rolls in the same second apart.
func (fs *fileSink) roll(at time.Time) error {
	fs.closeFile()
	fs.seq++
	name := fmt.Sprintf("events-%s-%03d.jsonl", at.UTC().Format("20060102-150405"), fs.seq)
	f, err := os.OpenFile(filepath.Join(fs.dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	fs.f = f
	fs.size = 0
	fs.day = at.UTC().Format("2006-01-02")
	return nil
}

func (fs *fileSink) closeFile() {
	if fs.f != nil {
		fs.f.Close()
		fs.f = nil
	}
}
