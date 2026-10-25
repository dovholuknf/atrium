package remoteaccess

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogMax is when the live log is rotated: it becomes LogFile.1, and one old file is kept.
const LogMax = 1 << 20

// LogLine is one open, close or refusal. Bytes are counted, never read, and nothing here is content.
type LogLine struct {
	// Event is open, close or refused.
	Event    string    `json:"event"`
	ID       string    `json:"id"`
	Caller   string    `json:"caller"`
	Via      string    `json:"via,omitempty"`
	Start    time.Time `json:"start,omitempty"`
	End      time.Time `json:"end,omitempty"`
	BytesIn  int64     `json:"bytes_in,omitempty"`
	BytesOut int64     `json:"bytes_out,omitempty"`
	EndedBy  string    `json:"ended_by,omitempty"`
	Error    string    `json:"error,omitempty"`
}

var logMu sync.Mutex

// AppendLog writes one JSON line, best effort: a log that cannot be written never fails a stream.
func AppendLog(dir string, l LogLine) {
	b, err := json.Marshal(l)
	if err != nil {
		return
	}
	logMu.Lock()
	defer logMu.Unlock()
	p := filepath.Join(dir, LogFile)
	if fi, err := os.Stat(p); err == nil && fi.Size() >= LogMax {
		_ = os.Remove(p + ".1")
		_ = os.Rename(p, p+".1")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

// RecentLog reads up to n lines, newest first, across the live file and the one old file.
func RecentLog(dir string, n int) []LogLine {
	logMu.Lock()
	defer logMu.Unlock()
	var all []LogLine
	for _, name := range []string{LogFile + ".1", LogFile} {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var l LogLine
			if json.Unmarshal(sc.Bytes(), &l) == nil {
				all = append(all, l)
			}
		}
		f.Close()
	}
	out := make([]LogLine, 0, n)
	for i := len(all) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, all[i])
	}
	return out
}
