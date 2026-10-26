package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dovholuknf/atrium/internal/base/remoteaccess"
	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/dovholuknf/atrium/internal/sshwizard"
)

// roomSSHCmd is `atrium room ssh`, the room's own side of turning ssh on. See docs/rnd/room-ssh-wizard-design.md.
//
// `setup --check` CHANGES NOTHING: it runs the wizard's checks and prints what each found. `setup` alone does the
// fix half of steps 5 to 7: the room's key, publishing it with the login user to the hub, and writing the set the hub
// relays into the login user's authorized keys. Everything else the wizard only checks, and a step that needs a
// person prints the command. `setup sync` is one quiet pass of steps 6 and 7, which the room also runs on a timer.
func roomSSHCmd() *cobra.Command {
	c := &cobra.Command{Use: "ssh", Short: "Turn ssh on for this room, step by step"}
	c.AddCommand(sshSetupCmd(), sshSyncCmd())
	return c
}

// sshSetupCmd is shared by `atrium room ssh setup` and `atrium ssh setup`.
func sshSetupCmd() *cobra.Command {
	var dir, user, from string
	var port int
	var check, asJSON, noSelf bool
	c := &cobra.Command{
		Use:   "setup",
		Short: "Make this room's ssh key, publish it, and install the others' keys (--check to only look)",
		Long: "Runs the eleven steps of the ssh wizard. Each one reads this machine and says what it found, and when\n" +
			"a person must act it prints the exact command.\n\n" +
			"Without --check it also does steps 5 to 7 itself: makes an ed25519 key with no passphrase (an agent\n" +
			"cannot type one), publishes it with the login user to the hub, and writes the keys the hub relays into\n" +
			"the login user's authorized_keys, inside a block marked `# BEGIN atrium`. Lines outside that block, a\n" +
			"person's own keys included, are never touched. --user sets the account other rooms log in as on this\n" +
			"machine (default claude) and the room keeps it. Not ssh config, not sshd and not this room's switch.\n\n" +
			"With --check nothing is written. Exits non-zero when a step failed, and zero otherwise, so a script reads\n" +
			"the steps with --json.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !check && (os.Getenv("ATRIUM_TASK_ID") != "" || os.Getenv("ATRIUM_AGENT_NAME") != "") {
				// A SPEED BUMP, NOT A BOUNDARY, as `remote-access on` has.
				return errors.New("this writes a key and authorized_keys, so a person at this machine runs it, not an agent. --check reads only")
			}
			opt := sshwizard.Options{Port: port, User: user, SelfTest: !noSelf}
			if from != "" {
				list, err := remoteaccess.ParseFrom(from)
				if err != nil {
					return err
				}
				opt.From = list
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			m := localSSHMachine(orDefault(dir, roomDir()), nil)
			var rep sshwizard.Report
			var acts []sshwizard.Action
			if check {
				rep = sshwizard.Check(ctx, m, opt)
			} else {
				rep, acts = sshwizard.Fix(ctx, m, opt)
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(struct {
					sshwizard.Report
					Did []sshwizard.Action `json:"did,omitempty"`
				}{rep, acts}); err != nil {
					return err
				}
			} else {
				for _, a := range acts {
					if a.Err != "" {
						fmt.Fprintf(out, "step %d: could not %s: %s\n", a.N, a.Did, a.Err)
					} else {
						fmt.Fprintf(out, "step %d: %s\n", a.N, a.Did)
					}
				}
				if len(acts) > 0 {
					fmt.Fprintln(out)
				}
				printSSHReport(out, rep, useColour(out))
			}
			for _, r := range rep.Results {
				if r.Status == sshwizard.Failed {
					return errAlreadySaid
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "where this machine's room keeps its certificate")
	c.Flags().BoolVar(&check, "check", false, "read only: print each step and what it found, and change nothing")
	c.Flags().BoolVar(&asJSON, "json", false, "print the steps as JSON")
	c.Flags().IntVar(&port, "port", 0, "the port of sshd (default: the room's switch, then 22)")
	c.Flags().StringVar(&user, "user", "", "the account other rooms log in as on this machine, kept by the room (default: the saved one, then claude)")
	c.Flags().StringVar(&from, "from", "", "who may open this room's sshd: hub, room names, or * (default: the room's switch)")
	c.Flags().BoolVar(&noSelf, "no-self-test", false, "leave out this room's own key and the log in to itself")
	return c
}

// localSSHMachine is this machine and this room's key directory. The hub's ssh record is read and written over the
// room's own link, and a hub that keeps none makes the steps that need it say so and wait.
func localSSHMachine(dir string, shared link.Dialer) sshwizard.LocalMachine {
	return localSSHMachineAt(dir, shared, daemon.ReadLocation)
}

func localSSHMachineAt(dir string, shared link.Dialer, location func() (daemon.Location, bool)) sshwizard.LocalMachine {
	hub := &hubSSH{dir: dir, shared: shared, location: location}
	return sshwizard.LocalMachine{
		KeyDir: dir,
		JoinedFn: func() sshwizard.Joined {
			saved, err := link.Keys{Dir: dir}.Joined()
			if err != nil {
				return sshwizard.Joined{Err: err}
			}
			j := sshwizard.Joined{Room: saved.Room, Hub: saved.Hub, Proven: saved.Transport == "direct" || saved.Proven}
			if !j.Proven {
				j.Rejoin = link.RejoinSentence(saved.Room)
			}
			return j
		},
		PublishFn: hub.published,
		PeersFn:   hub.peers,
		SendFn:    hub.publish,
	}
}

// hubSSH is this room's reading of the hub's ssh keys, kept for one command so the steps that each ask do not each
// dial. Publishing drops it, so the check after a fix reads what the hub holds now.
type hubSSH struct {
	dir      string
	shared   link.Dialer
	location func() (daemon.Location, bool)
	mu       sync.Mutex
	set      []link.SSHEntry
	err      error
	got      bool
}

// link is the dialer for one hub call and what to do with it afterwards. The room passes its own ladder, which is never
// closed here, because it holds the one ziti context the hub link uses. With none, a ladder is built for the call and
// closed after it, since a ziti dialer holds a context whose goroutines live until it is.
func (h *hubSSH) link() (string, link.Dialer, func(), error) {
	if h.shared != nil {
		saved, err := link.Keys{Dir: h.dir}.Joined()
		if err != nil {
			return "", nil, nil, err
		}
		return saved.Room, h.shared, func() {}, nil
	}
	room, d, err := roomLinkAt(h.dir, h.location)
	if err != nil {
		return "", nil, nil, err
	}
	return room, d, func() { closeDialer(d) }, nil
}

func (h *hubSSH) fetch(ctx context.Context) ([]link.SSHEntry, string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room, d, release, err := h.link()
	if err != nil {
		return nil, "", err
	}
	defer release()
	if !h.got {
		h.set, h.err = link.FetchSSHSet(ctx, d, room)
		h.got = true
	}
	if errors.Is(h.err, link.ErrNoSSHKeys) {
		return nil, room, sshwizard.ErrUnavailable
	}
	return h.set, room, h.err
}

func (h *hubSSH) published(ctx context.Context) (*sshwizard.Published, error) {
	set, room, err := h.fetch(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range set {
		if strings.EqualFold(e.Room, room) {
			return &sshwizard.Published{User: e.User, Port: e.Port, Enabled: true, On: e.On,
				Fingerprint: sshwizard.KeyFingerprint(e.Key)}, nil
		}
	}
	return nil, nil
}

func (h *hubSSH) peers(ctx context.Context) ([]sshwizard.PeerKey, error) {
	set, _, err := h.fetch(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]sshwizard.PeerKey, 0, len(set))
	for _, e := range set {
		out = append(out, sshwizard.PeerKey{Room: e.Room, User: e.User, Key: e.Key, Online: e.Online, SSHOn: e.On,
			Fingerprint: sshwizard.KeyFingerprint(e.Key)})
	}
	return out, nil
}

func (h *hubSSH) publish(ctx context.Context, p sshwizard.PublishRecord) error {
	room, d, release, err := h.link()
	if err != nil {
		return err
	}
	defer release()
	err = link.PublishSSH(ctx, d, room, link.SSHPublish{User: p.User, Key: p.Key, Port: p.Port, On: p.On})
	h.mu.Lock()
	h.got = false
	h.mu.Unlock()
	if errors.Is(err, link.ErrNoSSHKeys) {
		return sshwizard.ErrUnavailable
	}
	return err
}

// sshSyncCmd is `atrium room ssh setup sync`: one pass of steps 6 and 7 with no key made and nothing asked.
func sshSyncCmd() *cobra.Command {
	var dir string
	return &cobra.Command{
		Use:   "sync",
		Short: "Publish this room's key and install the hub's relayed keys, once",
		Long: "The pass the room runs every minute on its own: publishes this room's login user and key to the hub when\n" +
			"they differ from what it holds, and rewrites atrium's block of authorized_keys to the hub's set. It makes no\n" +
			"key. Run `atrium ssh setup` first.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dir == "" {
				dir = roomDir()
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			out := cmd.OutOrStdout()
			acts := sshSyncOnce(ctx, dir, nil)
			for _, a := range acts {
				fmt.Fprintf(out, "step %d: %s %s\n", a.N, a.Did, a.Err)
			}
			if len(acts) == 0 {
				fmt.Fprintln(out, "nothing to change")
			}
			return nil
		},
	}
}

// sshSyncOnce is one pass. A room with its switch on gets both steps, and a room with it off only publishes, since it
// has no sshd for the hub to open and nothing to authorize.
func sshSyncOnce(ctx context.Context, dir string, shared link.Dialer) []sshwizard.Action {
	st, err := remoteaccess.Load(dir)
	if err != nil {
		return nil
	}
	steps := []int{6}
	if st.On {
		steps = []int{6, 7}
	}
	// SelfTest keeps this room's own key in its block, as the wizard writes it. Sync never runs the self test itself.
	_, acts := sshwizard.Sync(ctx, localSSHMachine(dir, shared), sshwizard.Options{SelfTest: true}, steps...)
	return acts
}

// sshSyncLoop keeps this room's published record and its authorized keys current: a room added to the hub, or removed
// from it, reaches every room within a minute and nobody copies a key. It never makes a key, and a step that cannot
// be done is logged once per change and tried again.
func sshSyncLoop(ctx context.Context, dir string, shared link.Dialer) {
	t := time.NewTimer(20 * time.Second)
	defer t.Stop()
	last := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		cctx, cancel := context.WithTimeout(ctx, 40*time.Second)
		var parts []string
		for _, a := range sshSyncOnce(cctx, dir, shared) {
			parts = append(parts, fmt.Sprintf("step %d: %s %s", a.N, a.Did, a.Err))
		}
		cancel()
		if msg := strings.Join(parts, "; "); msg != last && msg != "" {
			log.Printf("[ssh] %s", msg)
			last = msg
		} else if msg == "" {
			last = ""
		}
		t.Reset(time.Minute)
	}
}

// sshWizardHandler is `GET /v1/ssh/wizard` on the room: the same checks, read only, as JSON. The query takes the
// same choices as the flags: port, user and from, and self_test=1 to run the self test, which is off here.
func sshWizardHandler(dir string, shared link.Dialer) func(r *http.Request) any {
	return func(r *http.Request) any {
		q := r.URL.Query()
		opt := sshwizard.Options{User: q.Get("user"), SelfTest: q.Get("self_test") == "1"}
		if p, err := strconv.Atoi(q.Get("port")); err == nil && p > 0 && p < 65536 {
			opt.Port = p
		}
		if f := q.Get("from"); f != "" {
			if list, err := remoteaccess.ParseFrom(f); err == nil {
				opt.From = list
			}
		}
		return sshwizard.Check(r.Context(), localSSHMachine(dir, shared), opt)
	}
}

func useColour(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && os.Getenv("NO_COLOR") == "" && term.IsTerminal(int(f.Fd()))
}

var sshMarks = map[sshwizard.Status]struct{ plain, colour string }{
	sshwizard.OK:         {"ok     ", "\x1b[32m●\x1b[0m ok     "},
	sshwizard.Todo:       {"todo   ", "\x1b[33m●\x1b[0m todo   "},
	sshwizard.NeedsHuman: {"HUMAN  ", "\x1b[33m●\x1b[0m \x1b[1mneeds you\x1b[0m"},
	sshwizard.Failed:     {"FAILED ", "\x1b[31m●\x1b[0m \x1b[31mfailed\x1b[0m "},
	sshwizard.Skipped:    {"waiting", "\x1b[90m●\x1b[0m waiting"},
}

func printSSHReport(w io.Writer, rep sshwizard.Report, colour bool) {
	who := rep.Room
	if who == "" {
		who = "this room"
	}
	fmt.Fprintf(w, "ssh for %s on %s\n\n", who, rep.OS)
	for _, r := range rep.Results {
		m := sshMarks[r.Status]
		mark := m.plain
		if colour {
			mark = m.colour
		}
		fmt.Fprintf(w, "%2d. %s  %s\n", r.N, mark, r.Name)
		fmt.Fprintf(w, "        %s\n", r.Found)
		for _, d := range r.Detail {
			fmt.Fprintf(w, "        - %s\n", d)
		}
		if r.Command != "" {
			fmt.Fprintf(w, "        run: %s\n", r.Command)
		}
	}
	fmt.Fprintf(w, "\n%s\n", strings.TrimSpace(rep.Summary))
}
