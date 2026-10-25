package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/base/sshcfg"
)

// `atrium ssh-proxy <room>` is ssh's ProxyCommand: stdin and stdout are the ssh protocol, carried through the hub to
// that room's own sshd. See docs/rnd/ssh-via-hub.md.
//
// IT WRITES NOTHING TO STDOUT BUT STREAM BYTES. ssh reads stdout as the protocol, so every refusal is one sentence on
// stderr and exit 255, which ssh prints above its own `Connection closed`. Atrium carries bytes only: it holds no key,
// password or agent socket, and the login is sshd's.

const streamUpgradeToken = "atrium-stream"

// proxyExit is what ssh sees when the stream could not be opened.
const proxyExit = 255

// proxyOpts are the doors and what to say to them.
type proxyOpts struct {
	room string
	// via is "hub", "room" or "" for the hub door first and the room door after.
	via string
	// hub is the hub's loopback board, room the agent listener of this machine's own room.
	hub, room2 string
	// hubAlt is the hub door tried after hub when hub is not there: the default board, when hub was found elsewhere.
	hubAlt string
	claim  string
	// parent ends the proxy when the ssh that started it is gone.
	parent parentWatch
}

// defaultHubBoard is where `atrium run` serves the hub's board unless told otherwise.
const defaultHubBoard = "http://127.0.0.1:7778"

// hubDoors is the hub board to try first and the one after it. An explicit --hub is the only one. Otherwise the first
// is what boardAddress finds (ATRIUM_BOARD_URL, or the board a local daemon or room recorded, which may be a room's own
// board and not the hub's) and the second is the default hub board, so a hub on its default port is found even when
// something else recorded itself.
func hubDoors(flag string) (first, alt string) {
	first = boardAddress(flag)
	if flag != "" || sameBoard(first, defaultHubBoard) {
		return first, ""
	}
	return first, defaultHubBoard
}

// sameBoard is whether two board addresses are the same loopback port.
func sameBoard(a, b string) bool {
	ua, ea := url.Parse(a)
	ub, eb := url.Parse(b)
	if ea != nil || eb != nil {
		return a == b
	}
	loop := func(h string) bool { return h == "localhost" || h == "127.0.0.1" || h == "::1" }
	if ua.Port() != ub.Port() {
		return false
	}
	return ua.Hostname() == ub.Hostname() || (loop(ua.Hostname()) && loop(ub.Hostname()))
}

func newSSHProxy() *cobra.Command {
	var via, hub string
	var printConfig bool
	c := &cobra.Command{
		Use:   "ssh-proxy <room>",
		Short: "ssh's ProxyCommand: reach a room's sshd through the hub",
		Long: "Carries ssh to <room>'s own sshd through the hub, over the links that room already holds.\n" +
			"The room has to have remote access on (atrium room remote-access on, at that machine).\n\n" +
			"Use it as: ssh -o ProxyCommand=\"atrium ssh-proxy %n\" user@<room>\n" +
			"or print a Host block to paste with --print-config, or write it with atrium ssh config add.\n\n" +
			"Atrium carries bytes only. The login is sshd's.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if printConfig {
				exe, err := os.Executable()
				if err != nil {
					exe = "atrium"
				}
				fmt.Fprint(cmd.OutOrStdout(), sshcfg.Block(args[0], exe, runtime.GOOS))
				return nil
			}
			if via != "" && via != "hub" && via != "room" {
				fmt.Fprintln(cmd.ErrOrStderr(), "--via is hub or room")
				os.Exit(proxyExit)
			}
			o := proxyOpts{room: strings.TrimSuffix(args[0], sshcfg.Suffix), via: via, claim: callerClaim(), room2: hubAddress(""), parent: watchParent()}
			o.hub, o.hubAlt = hubDoors(hub)
			code := runProxy(os.Stdin, os.Stdout, cmd.ErrOrStderr(), o)
			os.Exit(code)
			return nil
		},
	}
	c.Flags().StringVar(&via, "via", "", "force a door: hub (this machine runs the hub) or room (this machine's own room)")
	c.Flags().StringVar(&hub, "hub", "", "the hub's loopback board, when it is not the recorded or default one")
	c.Flags().BoolVar(&printConfig, "print-config", false, "print an ssh config Host block for the room, for this machine's OS")
	return c
}

// callerClaim is what this caller says about itself: user@host. A claim, recorded as one, never trusted.
func callerClaim() string {
	name := "unknown"
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
		if i := strings.LastIndex(name, `\`); i >= 0 {
			name = name[i+1:]
		}
	}
	return name + "@" + hostname()
}

// errNoDoor is a door that is not there, so the next may be tried.
var errNoDoor = errors.New("no door")

// refusalError is a sentence from the other end, to be said as it is.
type refusalError struct{ msg string }

func (e *refusalError) Error() string { return e.msg }

// runProxy opens the stream and joins it to stdin and stdout. It returns the exit code and says every refusal on
// stderr as one sentence.
func runProxy(stdin io.Reader, stdout, stderr io.Writer, o proxyOpts) int {
	var conn net.Conn
	var br *bufio.Reader
	var err error

	tryHub, tryRoom := o.via != "room", o.via != "hub"
	var hubErr error
	if tryHub {
		conn, br, err = openDoor(o.hub, "/_hub/ssh/"+url.PathEscape(o.room), o)
		if errors.Is(err, errNoDoor) && o.hubAlt != "" {
			conn, br, err = openDoor(o.hubAlt, "/_hub/ssh/"+url.PathEscape(o.room), o)
		}
		hubErr = err
		// The hub door says it is not there with a refused connection or a 404. Anything else is an answer.
		if err != nil && !errors.Is(err, errNoDoor) {
			return say(stderr, err)
		}
	}
	if conn == nil && tryRoom {
		conn, br, err = openDoor(o.room2, "/stream", o)
		if err != nil && !errors.Is(err, errNoDoor) {
			return say(stderr, err)
		}
	}
	if conn == nil {
		switch {
		case o.via == "hub" && hubErr != nil:
			fmt.Fprintln(stderr, "no atrium hub is running on this machine (or it does not carry ssh streams yet. update the hub)")
		default:
			fmt.Fprintln(stderr, "no atrium hub or room is running on this machine")
		}
		return proxyExit
	}
	defer conn.Close()
	joinStdio(stdin, stdout, conn, br, o.parent)
	return 0
}

func say(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, err.Error())
	return proxyExit
}

// openDoor asks one door for the stream. The hub door names the room in the path and the room door in a header, and
// both answer 101 and then carry ssh's bytes.
func openDoor(base, path string, o proxyOpts) (net.Conn, *bufio.Reader, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return nil, nil, errNoDoor
	}
	conn, err := net.DialTimeout("tcp", u.Host, 3*time.Second)
	if err != nil {
		return nil, nil, errNoDoor
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "POST %s HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: %s\r\n", path, u.Host, streamUpgradeToken)
	if path == "/stream" {
		fmt.Fprintf(&sb, "X-Atrium-Room: %s\r\n", o.room)
	}
	if c := cleanHeader(o.claim); c != "" {
		fmt.Fprintf(&sb, "X-Atrium-Claim: %s\r\n", c)
	}
	sb.WriteString("\r\n")
	// Bounded while the other end answers, and cleared before ssh's bytes begin.
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
	if _, err := io.WriteString(conn, sb.String()); err != nil {
		conn.Close()
		return nil, nil, errNoDoor
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodPost})
	if err != nil {
		conn.Close()
		return nil, nil, errNoDoor
	}
	if resp.StatusCode == http.StatusSwitchingProtocols {
		_ = conn.SetDeadline(time.Time{})
		return conn, br, nil
	}
	defer conn.Close()
	var body struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&body)
	// ONLY A SENTENCE IS AN ANSWER. A 404, or a page from a listener that is not the hub's loopback door (a room's own
	// board answers this path too, with its own page), means this door is not there, and the next is tried.
	if body.Error == "" {
		return nil, nil, errNoDoor
	}
	return nil, nil, &refusalError{msg: body.Error}
}

func cleanHeader(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// parentCheckEvery is how often the proxy asks whether the ssh that started it is still there.
const parentCheckEvery = 30 * time.Second

// parentWatch ends the proxy when gone reports true, checked every `every`. A nil gone watches nothing.
type parentWatch struct {
	gone  func() bool
	every time.Duration
	drain time.Duration
}

// drainFor is the stdin drain this watch asks for, stdinDrain when it names none.
func (w parentWatch) drainFor() time.Duration {
	if w.drain > 0 {
		return w.drain
	}
	return stdinDrain
}

// watchParent is the parent ssh of this process: the pid at start, asked about again each parentCheckEvery.
func watchParent() parentWatch {
	ppid := os.Getppid()
	return parentWatch{gone: func() bool { return !pidAlive(ppid) }, every: parentCheckEvery}
}

// stdinDrain is how long the stream may keep answering after ssh closed its end, so the last bytes still reach ssh.
const stdinDrain = 5 * time.Second

// joinStdio is the whole of the proxy's work: stdin to the stream, the stream to stdout. It returns when EITHER
// direction ends or the parent ssh is gone, and closes the stream so the other copy stops: an orphaned proxy would
// otherwise wait on a far side that may never speak again.
func joinStdio(stdin io.Reader, stdout io.Writer, conn net.Conn, br *bufio.Reader, w parentWatch) {
	inDone := make(chan struct{})
	outDone := make(chan struct{})
	go func() {
		defer close(inDone)
		_, _ = io.CopyBuffer(conn, stdin, make([]byte, 32<<10))
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
	}()
	go func() {
		defer close(outDone)
		_, _ = io.CopyBuffer(stdout, br, make([]byte, 32<<10))
	}()
	defer conn.Close()
	var gone <-chan time.Time
	if w.gone != nil && w.every > 0 {
		t := time.NewTicker(w.every)
		defer t.Stop()
		gone = t.C
	}
	var drain <-chan time.Time
	in := (<-chan struct{})(inDone)
	for {
		select {
		case <-outDone:
			return
		case <-in:
			in = nil
			drain = time.After(w.drainFor())
		case <-drain:
			return
		case <-gone:
			if w.gone() {
				return
			}
		}
	}
}
