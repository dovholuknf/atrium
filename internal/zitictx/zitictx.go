// Package zitictx is the one place atrium builds an OpenZiti context.
//
// A ziti context starts goroutines that live until it is closed: the api session refresh, the posture cache, the
// router channels. So a process holds exactly ONE per identity, opened the first time anything needs it and kept for
// the life of the process. Everything that talks to the overlay (the hub link, the board's native listener, the
// capabilities question) asks for it here and gets the same one, and a second build for an identity is refused and
// logged as the bug it is.
package zitictx

import (
	"errors"
	"log"
	"path/filepath"
	"sync"

	"github.com/openziti/sdk-golang/ziti"
)

var (
	mu   sync.Mutex
	open = map[string]*counted{}
)

// Live is how many contexts built here have not been closed.
func Live() int {
	mu.Lock()
	defer mu.Unlock()
	return len(open)
}

// ErrDuplicate is what New answers for an identity that already has a context.
var ErrDuplicate = errors.New("a second ziti context for one identity was refused. that is a bug")

var (
	loadConfig = ziti.NewConfigFromFile
	newContext = ziti.NewContext
)

// SetSDK replaces how an identity file is read and a context is built, and returns what puts them back. For tests,
// which have no controller.
func SetSDK(load func(string) (*ziti.Config, error), mk func(*ziti.Config) (ziti.Context, error)) (restore func()) {
	mu.Lock()
	defer mu.Unlock()
	l, m := loadConfig, newContext
	loadConfig, newContext = load, mk
	return func() {
		mu.Lock()
		defer mu.Unlock()
		loadConfig, newContext = l, m
	}
}

type counted struct {
	ziti.Context
	key  string
	who  string
	once sync.Once
}

// Close closes the context once and frees its identity to be built again.
func (c *counted) Close() {
	c.once.Do(func() {
		c.Context.Close()
		mu.Lock()
		delete(open, c.key)
		n := len(open)
		mu.Unlock()
		log.Printf("[ziti] context closed (%s), %d live", c.who, n)
	})
}

// shared is what everything but the owner holds: the context, and no way to close it.
type shared struct {
	*counted
}

// Close does nothing. The context belongs to the process, and CloseAll ends it.
func (shared) Close() {}

// Relogin makes the context authenticate again, on the same context and the same goroutines.
func (s shared) Relogin() error { return relogin(s.counted) }

func relogin(c *counted) error {
	if r, ok := c.Context.(interface{ Reauthenticate() error }); ok {
		return r.Reauthenticate()
	}
	return c.Context.Authenticate()
}

func keyOf(identity string) string {
	if abs, err := filepath.Abs(identity); err == nil {
		identity = abs
	}
	return filepath.Clean(identity)
}

// New builds the context for key, and refuses when it already has one. Use SharedFile, which builds at most once.
func New(who, key string, cfg *ziti.Config) (ziti.Context, error) {
	c, err := build(who, key, cfg)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func build(who, key string, cfg *ziti.Config) (*counted, error) {
	mu.Lock()
	if _, ok := open[key]; ok {
		mu.Unlock()
		log.Printf("[ziti] BUG: refusing a second context (%s) for %s", who, key)
		return nil, ErrDuplicate
	}
	mk := newContext
	mu.Unlock()
	c, err := mk(cfg)
	if err != nil {
		return nil, err
	}
	cc := &counted{Context: c, key: key, who: who}
	mu.Lock()
	if _, ok := open[key]; ok {
		mu.Unlock()
		c.Close()
		log.Printf("[ziti] BUG: refusing a second context (%s) for %s", who, key)
		return nil, ErrDuplicate
	}
	open[key] = cc
	n := len(open)
	mu.Unlock()
	log.Printf("[ziti] context built (%s), %d live", who, n)
	return cc, nil
}

// SharedFile is the process's one context for an identity file, built the first time and handed to every caller after.
// Its Close does nothing.
func SharedFile(who, identity string) (ziti.Context, error) {
	key := keyOf(identity)
	mu.Lock()
	if c, ok := open[key]; ok {
		mu.Unlock()
		return shared{c}, nil
	}
	load := loadConfig
	mu.Unlock()
	cfg, err := load(identity)
	if err != nil {
		return nil, err
	}
	c, err := build(who, key, cfg)
	if errors.Is(err, ErrDuplicate) {
		mu.Lock()
		c, ok := open[key]
		mu.Unlock()
		if ok {
			return shared{c}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return shared{c}, nil
}

// Relogin authenticates an identity's context again, in place. A context that does not exist yet is not built.
func Relogin(identity string) error {
	mu.Lock()
	c, ok := open[keyOf(identity)]
	mu.Unlock()
	if !ok {
		return nil
	}
	return relogin(c)
}

// CloseAll closes every context, for the process that is stopping.
func CloseAll() {
	mu.Lock()
	all := make([]*counted, 0, len(open))
	for _, c := range open {
		all = append(all, c)
	}
	mu.Unlock()
	for _, c := range all {
		c.Close()
	}
}
