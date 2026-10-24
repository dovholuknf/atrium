// Package sshwizard is the step engine behind turning ssh on for a room. See docs/rnd/room-ssh-wizard-design.md.
//
// THIS PART ONLY READS. A Check looks at the machine and says what it found, and nothing here writes a key, an
// authorized_keys file, an ssh config or the room's switch. The fixes are a later step and live in their own file.
//
// WHICH SSHD ANSWERS IS DECIDED BY IDENTITY, NOT BY A PROCESS NAME. This machine's sshd has host key files that any
// user can read. An address answers as this machine's sshd when the host key it offers is one of those keys. A banner that
// names another system is only supporting text, because wslrelay (WSL's localhost forwarding) holds 127.0.0.1:22 on a
// Windows machine and the Windows sshd holds only the wildcard bind, and the more specific bind wins.
package sshwizard

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/dovholuknf/atrium/internal/base/nowindow"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Env is everything the wizard asks of the machine, so a test can answer it. It is the seam internal/guard/env.go
// uses, and every method only reads.
type Env interface {
	GOOS() string
	// Keyscan asks host:port for its version banner and each host key it offers, and logs in to nothing.
	Keyscan(ctx context.Context, host string, port int) (Scan, error)
	// Glob lists the files matching a pattern.
	Glob(pattern string) []string
	ReadFile(path string) ([]byte, error)
	Getenv(key string) string
	Hostname() string
	// Addrs is the machine's own unicast addresses, loopback included.
	Addrs() []string
	// ListenerOwner names the process holding a listening TCP port on addr, "" when it cannot be said.
	ListenerOwner(ctx context.Context, addr string, port int) string
}

// HostKey is one public host key.
type HostKey struct {
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
}

// Scan is what one sshd said before authentication: its version banner and its host keys.
type Scan struct {
	Banner string
	Keys   []HostKey
}

// Probe is what one address answered.
type Probe struct {
	Addr   string    `json:"addr"`
	Port   int       `json:"port"`
	Banner string    `json:"banner,omitempty"`
	Keys   []HostKey `json:"keys,omitempty"`
	// Err is why nothing answered: refused, timed out, or no host key offered.
	Err string `json:"err,omitempty"`
}

// Verdict is what an address turned out to be.
type Verdict string

const (
	// Mine is an address whose host key is one of this machine's own.
	Mine Verdict = "mine"
	// Other answered ssh with a key that is not this machine's.
	Other Verdict = "other"
	// Nothing did not answer ssh.
	Nothing Verdict = "nothing"
	// Unknown answered ssh and this machine's own host keys could not be read to compare.
	Unknown Verdict = "unknown"
)

// Verdict compares the probe against this machine's own host keys.
func (p Probe) Verdict(local []HostKey) Verdict {
	if len(p.Keys) == 0 {
		return Nothing
	}
	if len(local) == 0 {
		return Unknown
	}
	for _, k := range p.Keys {
		for _, l := range local {
			if k.Type == l.Type && k.Fingerprint == l.Fingerprint {
				return Mine
			}
		}
	}
	return Other
}

// Foreign says whether the banner names another system, which is what an Ubuntu sshd behind wslrelay says. It is a
// hint for the sentence only. The key decides.
func (p Probe) Foreign() bool {
	b := strings.ToLower(p.Banner)
	for _, w := range []string{"ubuntu", "debian", "fedora", "centos", "alpine", "raspbian", "freebsd", "openbsd"} {
		if strings.Contains(b, w) {
			return true
		}
	}
	return false
}

// Fingerprint is the SHA256 form ssh prints, `SHA256:` and unpadded base64 of the key blob.
func Fingerprint(blob []byte) string {
	sum := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// parseKeyLine reads `[host] type base64 [comment]` and `type base64 [comment]`.
func parseKeyLine(line string) (HostKey, bool) {
	f := strings.Fields(line)
	for i := 0; i < len(f) && i < 2; i++ {
		if !strings.HasPrefix(f[i], "ssh-") && !strings.HasPrefix(f[i], "ecdsa-") && !strings.HasPrefix(f[i], "sk-") {
			continue
		}
		if i+1 >= len(f) {
			return HostKey{}, false
		}
		blob, err := base64.StdEncoding.DecodeString(f[i+1])
		if err != nil || len(blob) == 0 {
			return HostKey{}, false
		}
		return HostKey{Type: f[i], Fingerprint: Fingerprint(blob)}, true
	}
	return HostKey{}, false
}

// scanTimeout bounds one scan. The wizard's own bound is longer than a few of these.
const scanTimeout = 4 * time.Second

// ProbeAddr asks addr:port who it is.
func ProbeAddr(ctx context.Context, env Env, addr string, port int) Probe {
	p := Probe{Addr: addr, Port: port}
	ctx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()
	sc, err := env.Keyscan(ctx, addr, port)
	p.Banner, p.Keys = sc.Banner, sc.Keys
	if len(p.Keys) == 0 {
		switch {
		case err != nil:
			p.Err = err.Error()
		case p.Banner != "":
			p.Err = "answered " + p.Banner + " and offered no host key"
		default:
			p.Err = "nothing answered ssh"
		}
	}
	return p
}

// HostKeyGlobs are where this machine's sshd keeps its public host keys.
func HostKeyGlobs(env Env) []string {
	if env.GOOS() == "windows" {
		pd := env.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return []string{joinFor("windows", pd, "ssh", "ssh_host_*_key.pub")}
	}
	return []string{"/etc/ssh/ssh_host_*_key.pub"}
}

// HostKeys reads this machine's sshd host keys. They are public and any user may read them.
func HostKeys(env Env) []HostKey {
	var out []HostKey
	for _, g := range HostKeyGlobs(env) {
		for _, path := range env.Glob(g) {
			b, err := env.ReadFile(path)
			if err != nil {
				continue
			}
			if k, ok := parseKeyLine(strings.TrimSpace(string(b))); ok {
				out = append(out, k)
			}
		}
	}
	return out
}

// Candidates are the addresses worth asking, in the order a person would rather have them: loopback, then the
// machine's name, then each other address.
func Candidates(env Env) []string {
	seen := map[string]bool{}
	var out []string
	add := func(a string) {
		if a != "" && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	add("127.0.0.1")
	add("::1")
	add(env.Hostname())
	var rest []string
	for _, a := range env.Addrs() {
		if ip := net.ParseIP(a); ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			rest = append(rest, a)
		}
	}
	sort.Strings(rest)
	for _, a := range rest {
		add(a)
	}
	return out
}

// Finding is the whole answer to "which sshd is this machine's".
type Finding struct {
	Port   int       `json:"port"`
	Local  []HostKey `json:"local_keys"`
	Probes []Probe   `json:"probes"`
	// Chosen is the address to dial, "" when none of them is this machine's sshd.
	Chosen string `json:"chosen,omitempty"`
	// Pinned says the choice is a literal IP, which can move with DHCP, because no name matched.
	Pinned bool `json:"pinned,omitempty"`
	// Notes are plain sentences about each address, one per line the human reads.
	Notes []string `json:"notes"`
}

// Which scans every candidate and chooses. Loopback wins when it is this machine's, because it never moves. Else the
// machine's name when it resolves to a matching key, and only then a literal address.
func Which(ctx context.Context, env Env, port int) Finding {
	f := Finding{Port: port, Local: HostKeys(env)}
	cands := Candidates(env)
	f.Probes = make([]Probe, len(cands))
	var wg sync.WaitGroup
	for i, a := range cands {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.Probes[i] = ProbeAddr(ctx, env, a, port)
		}()
	}
	wg.Wait()
	for _, p := range f.Probes {
		v := p.Verdict(f.Local)
		if v == Mine && f.Chosen == "" {
			f.Chosen = p.Addr
			f.Pinned = net.ParseIP(p.Addr) != nil && !net.ParseIP(p.Addr).IsLoopback()
		}
		f.Notes = append(f.Notes, describe(ctx, env, p, v))
	}
	return f
}

func describe(ctx context.Context, env Env, p Probe, v Verdict) string {
	where := net.JoinHostPort(p.Addr, fmt.Sprint(p.Port))
	switch v {
	case Mine:
		return where + " is this machine's sshd"
	case Nothing:
		return where + ": " + p.Err
	case Unknown:
		return where + " answers " + orBanner(p.Banner) + ", and this machine's host keys could not be read to compare"
	}
	s := where + " is not this machine's sshd: it answers " + orBanner(p.Banner) + " with a host key this machine does not have"
	if owner := env.ListenerOwner(ctx, p.Addr, p.Port); owner != "" {
		s += " (held by " + owner + ")"
		if strings.Contains(strings.ToLower(owner), "wslrelay") {
			s = where + " is WSL's sshd (wslrelay), not " + osName(env) + "'s"
		}
	}
	return s
}

func osName(env Env) string {
	switch env.GOOS() {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	}
	return "this machine"
}

func orBanner(b string) string {
	if b == "" {
		return "ssh"
	}
	return b
}

// OSEnv is the real machine.
type OSEnv struct{}

func (OSEnv) GOOS() string                      { return runtime.GOOS }
func (OSEnv) Glob(p string) []string            { m, _ := filepath.Glob(p); return m }
func (OSEnv) ReadFile(p string) ([]byte, error) { return os.ReadFile(p) }
func (OSEnv) Getenv(k string) string            { return os.Getenv(k) }

func (OSEnv) Hostname() string {
	h, _ := os.Hostname()
	return h
}

func (OSEnv) Addrs() []string {
	var out []string
	as, _ := net.InterfaceAddrs()
	for _, a := range as {
		if n, ok := a.(*net.IPNet); ok {
			out = append(out, n.IP.String())
		}
	}
	return out
}

// dialTimeout bounds one connection, as `ssh-keyscan -T 3` did.
const dialTimeout = 3 * time.Second

// hostKeyAlgos are asked for one per connection, because a server offers only the key of the algorithm agreed on.
var hostKeyAlgos = []string{ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521, ssh.KeyAlgoRSASHA512}

var errGotHostKey = errors.New("host key read, stopping before authentication")

// Keyscan connects once per host key algorithm, one after another so a probe never holds more than one of sshd's
// unauthenticated slots, and stops at the first connection that gets no ssh banner.
func (OSEnv) Keyscan(ctx context.Context, host string, port int) (Scan, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	var sc Scan
	var first error
	for _, algo := range hostKeyAlgos {
		banner, key, err := hostKeyOnce(ctx, addr, algo)
		if sc.Banner == "" {
			sc.Banner = banner
		}
		if key != nil {
			sc.Keys = append(sc.Keys, HostKey{Type: key.Type(), Fingerprint: Fingerprint(key.Marshal())})
			continue
		}
		if first == nil {
			first = err
		}
		if banner == "" || ctx.Err() != nil {
			break
		}
	}
	if len(sc.Keys) > 0 {
		return sc, nil
	}
	return sc, first
}

func hostKeyOnce(ctx context.Context, addr, algo string) (string, ssh.PublicKey, error) {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", nil, err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	defer context.AfterFunc(ctx, func() { conn.Close() })()
	vc := &versionConn{Conn: conn}
	var mu sync.Mutex
	var key ssh.PublicKey
	cfg := &ssh.ClientConfig{
		User:              "atrium",
		HostKeyAlgorithms: []string{algo},
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			mu.Lock()
			key = k
			mu.Unlock()
			return errGotHostKey
		},
		Timeout: dialTimeout,
	}
	if c, _, _, err2 := ssh.NewClientConn(vc, addr, cfg); err2 != nil {
		err = err2
	} else {
		c.Close()
	}
	mu.Lock()
	defer mu.Unlock()
	if key != nil {
		err = nil
	}
	return vc.version(), key, err
}

// versionConn keeps the first bytes the server sends, which hold its `SSH-2.0-...` line.
type versionConn struct {
	net.Conn
	mu   sync.Mutex
	head []byte
}

func (v *versionConn) Read(b []byte) (int, error) {
	n, err := v.Conn.Read(b)
	v.mu.Lock()
	if room := 1024 - len(v.head); room > 0 && n > 0 {
		v.head = append(v.head, b[:min(n, room)]...)
	}
	v.mu.Unlock()
	return n, err
}

func (v *versionConn) version() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, l := range bytes.Split(v.head, []byte("\n")) {
		if bytes.HasPrefix(l, []byte("SSH-")) {
			return strings.TrimSpace(string(l))
		}
	}
	return ""
}

// ListenerOwner asks Windows for the owning process of a listener, which a standard user may read. Elsewhere it says
// nothing: the key already decided, and this is only supporting text.
func (OSEnv) ListenerOwner(ctx context.Context, addr string, port int) string {
	if runtime.GOOS != "windows" || net.ParseIP(addr) == nil {
		return ""
	}
	script := fmt.Sprintf("Get-NetTCPConnection -LocalPort %d -State Listen -ErrorAction SilentlyContinue | "+
		"Where-Object { $_.LocalAddress -eq '%s' } | Select-Object -First 1 -ExpandProperty OwningProcess | "+
		"ForEach-Object { (Get-Process -Id $_ -ErrorAction SilentlyContinue).ProcessName }", port, addr)
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	nowindow.Hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Describe is the one line `atrium room remote-access` prints for what the room dials: the banner, the key's
// fingerprint, and whether that is this machine's own sshd.
func Describe(ctx context.Context, env Env, addr string, port int) string {
	p := ProbeAddr(ctx, env, addr, port)
	where := net.JoinHostPort(addr, fmt.Sprint(port))
	v := p.Verdict(HostKeys(env))
	if v == Nothing {
		return "dials " + where + ": " + p.Err
	}
	s := "dials " + where + ": " + orBanner(p.Banner) + ", " + p.Keys[0].Type + " " + p.Keys[0].Fingerprint
	switch v {
	case Mine:
		return s + ", this machine's own sshd"
	case Other:
		return s + ", NOT this machine's sshd. run: atrium room ssh setup --check"
	}
	return s + ", host keys on this machine could not be read to compare"
}

// joinFor joins path parts the way goos does, not the way the machine running this does. The wizard reads another
// OS's layout in tests, and filepath.Join on Linux would turn `C:\ProgramData` and `ssh` into a path no Windows
// machine has.
func joinFor(goos string, parts ...string) string {
	if goos != "windows" {
		return filepath.Join(parts...)
	}
	return strings.Join(parts, `\`)
}

// KeyFingerprint is the fingerprint of a public key line, "" when it is not one.
func KeyFingerprint(line string) string {
	k, ok := parseKeyLine(line)
	if !ok {
		return ""
	}
	return k.Fingerprint
}
