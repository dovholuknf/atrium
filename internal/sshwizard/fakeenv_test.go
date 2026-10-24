package sshwizard

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// fakeEnv answers for a machine. Scans maps "host:port" to what ssh-keyscan would print, and a missing entry is a
// refused connection.
type fakeEnv struct {
	goos      string
	env       map[string]string
	host      string
	addrs     []string
	files     map[string]string // path to contents
	scans     map[string]string
	owners    map[string]string
	scanCalls []string
	mu        sync.Mutex
}

func (f *fakeEnv) GOOS() string { return f.goos }
func (f *fakeEnv) Keyscan(_ context.Context, host string, port int) (Scan, error) {
	k := fmt.Sprintf("%s:%d", host, port)
	f.mu.Lock()
	f.scanCalls = append(f.scanCalls, k)
	f.mu.Unlock()
	if out, ok := f.scans[k]; ok {
		return parseScan(out), nil
	}
	return Scan{}, errors.New("connect: connection refused")
}

// parseScan reads `# host banner` and `host type base64` lines, the text ssh-keyscan prints, which is how a fake says it.
func parseScan(out string) Scan {
	var sc Scan
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			if i := strings.Index(line, "SSH-"); i >= 0 && sc.Banner == "" {
				sc.Banner = strings.TrimSpace(line[i:])
			}
			continue
		}
		if k, ok := parseKeyLine(line); ok {
			sc.Keys = append(sc.Keys, k)
		}
	}
	return sc
}
func (f *fakeEnv) Glob(pattern string) []string {
	pre := pattern[:strings.Index(pattern, "*")]
	var out []string
	for p := range f.files {
		if strings.HasPrefix(p, pre) && strings.HasSuffix(p, ".pub") {
			out = append(out, p)
		}
	}
	return out
}
func (f *fakeEnv) ReadFile(p string) ([]byte, error) {
	if s, ok := f.files[p]; ok {
		return []byte(s), nil
	}
	return nil, errors.New("not found")
}
func (f *fakeEnv) Getenv(k string) string { return f.env[k] }
func (f *fakeEnv) Hostname() string       { return f.host }
func (f *fakeEnv) Addrs() []string        { return f.addrs }
func (f *fakeEnv) ListenerOwner(_ context.Context, addr string, port int) string {
	return f.owners[fmt.Sprintf("%s:%d", addr, port)]
}

// blob is a stand in key body, distinct per name.
func blob(name string) string { return base64.StdEncoding.EncodeToString([]byte("key-body-" + name)) }

func keyLine(typ, name string) string { return typ + " " + blob(name) }

func scanOut(banner, typ, name string) string {
	return "# h SSH-2.0-" + banner + "\nh " + keyLine(typ, name) + "\n"
}

func fp(name string) string { return Fingerprint([]byte("key-body-" + name)) }
