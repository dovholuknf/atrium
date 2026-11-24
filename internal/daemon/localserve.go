package daemon

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/base/detach"
	"github.com/dovholuknf/atrium/internal/base/userenv"
	"github.com/dovholuknf/atrium/internal/store"
)

// LocalServeEvery is how often the room checks the servers in `local_servers`.
var LocalServeEvery = 30 * time.Second

// localServeGrace is how long a server the room started may take to answer before it is started again: a model
// server loads for a minute or more before its port opens.
const localServeGrace = 5 * time.Minute

// localKeeper starts each server in `local_servers` that does not answer, detached so it outlives a room restart.
type localKeeper struct {
	mu      sync.Mutex
	started map[string]time.Time
	up      func(ctx context.Context, url string) bool
	start   func(s store.LocalServer, logPath string) error
}

func newLocalKeeper() *localKeeper {
	return &localKeeper{started: map[string]time.Time{}, up: healthAnswers, start: startLocalServer}
}

// tick starts what is down and was not started within the grace, and returns the names it started.
func (k *localKeeper) tick(ctx context.Context, now time.Time, servers []store.LocalServer, logDir string) []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []string
	for _, s := range servers {
		if k.up(ctx, s.Health) {
			continue
		}
		if at, ok := k.started[s.Name]; ok && now.Sub(at) < localServeGrace {
			continue
		}
		k.started[s.Name] = now
		logPath := filepath.Join(logDir, "local-"+s.Name+".log")
		if err := k.start(s, logPath); err != nil {
			log.Printf("[atrium] local server %s did not start: %v", s.Name, err)
			continue
		}
		log.Printf("[atrium] local server %s was not answering at %s, started it, log %s", s.Name, s.Health,
			filepath.ToSlash(logPath))
		out = append(out, s.Name)
	}
	return out
}

// healthAnswers is any HTTP answer: llama-server says 503 while it loads, and that is up.
func healthAnswers(ctx context.Context, url string) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	res.Body.Close()
	return true
}

func startLocalServer(s store.LocalServer, logPath string) error {
	exe := s.Cmd
	if p, err := userenv.LookPath(exe); err == nil {
		exe = p
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return err
	}
	p, err := detach.Start(exe, s.Args, logPath)
	if err != nil {
		return err
	}
	return p.Release()
}

// localServeLoop keeps `local_servers` up, read fresh each tick so a change applies without a restart.
func (d *Daemon) localServeLoop(ctx context.Context) {
	k := newLocalKeeper()
	logDir := filepath.Join(filepath.Dir(d.opts.DBPath), "logs")
	for {
		servers, err := d.st.LocalServers()
		if err != nil {
			log.Printf("[atrium] local servers: %v", err)
		}
		k.tick(ctx, time.Now(), servers, logDir)
		select {
		case <-ctx.Done():
			return
		case <-time.After(LocalServeEvery):
		}
	}
}
