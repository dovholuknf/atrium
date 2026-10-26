package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/base/remoteaccess"
	"github.com/dovholuknf/atrium/internal/sshwizard"
)

// roomRemoteAccessCmd is `atrium room remote-access`, this room's switch for letting the hub carry an ssh stream to
// its own sshd. See docs/rnd/ssh-via-hub.md.
//
// ONLY THIS COMMAND WRITES THE FILE, on this machine. The hub, the board, the room's api and the agent listener have
// no route that does, and that is the point: the room decides.
func roomRemoteAccessCmd() *cobra.Command {
	var dir, running, resolved string
	where := func() string { return resolved }

	c := &cobra.Command{
		Use:   "remote-access",
		Short: "Whether the hub may carry an ssh connection to this machine's sshd",
		Long: "Prints the state: on or off, the port, who may open it and how many streams are open.\n\n" +
			"Off unless a person at this machine turns it on. When it is on, the hub forwards a TCP\n" +
			"stream to this machine's sshd (127.0.0.1:<port>, or --addr)  and nothing else. Atrium carries bytes only:\n" +
			"the login is sshd's, and no key, password or agent socket passes through atrium.",
		Args: cobra.NoArgs,
		// Runs before every subcommand too, so none of them can write into a directory no room lives in.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			running = runningRoomDir()
			d, _, err := pickRoomDir(dir, running)
			resolved = d
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			st := remoteaccess.Effective(where())
			fmt.Fprintln(out, "remote access: "+st.Describe())
			fmt.Fprintln(out, dirLine(where(), running))
			if st.Err == nil && st.On && st.SetAt != "" {
				fmt.Fprintf(out, "turned on %s by %s\n", st.SetAt, orDefaultWord(st.SetBy, "unknown"))
			}
			if st.On {
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
				fmt.Fprintln(out, sshwizard.Describe(ctx, sshwizard.OSEnv{}, st.Host(), st.Port))
				cancel()
			}
			if n, ok := openStreams(); ok {
				fmt.Fprintf(out, "streams open: %d\n", n)
			}
			if !st.On && !st.Forced && st.Err == nil {
				fmt.Fprintln(out, "turn it on with: atrium room remote-access on")
			}
			return nil
		},
	}
	c.PersistentFlags().StringVar(&dir, "dir", "", "where this machine's room keeps its certificate")

	var from string
	var port int
	var addr, user string
	on := &cobra.Command{
		Use:   "on",
		Short: "Let the hub carry ssh to this machine's sshd",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// A SPEED BUMP, NOT A BOUNDARY. An agent could edit the file, as it could edit sshd_config, and the
			// permission chain already stands in front of both.
			if os.Getenv("ATRIUM_TASK_ID") != "" || os.Getenv("ATRIUM_AGENT_NAME") != "" {
				return errors.New("remote access is turned on by a person at this machine, not by an agent")
			}
			list, err := remoteaccess.ParseFrom(from)
			if err != nil {
				return err
			}
			if port < 1 || port > 65535 {
				return errors.New("--port must be 1 to 65535")
			}
			if addr != "" && !remoteaccess.ValidAddr(addr) {
				return errors.New("--addr must be an IP address or a host name")
			}
			if addr == "" {
				addr = pickSSHDAddr(cmd.OutOrStdout(), port)
			}
			if addr == remoteaccess.DefaultAddr {
				addr = ""
			}
			s := remoteaccess.State{On: true, Addr: addr, Port: port, From: list, SetBy: remoteWho()}
			// THE LOGIN USER IS KEPT ACROSS ON AND OFF: it is a standing choice, and turning on is not a decision to
			// forget it. --user changes it.
			if cur, err := remoteaccess.Load(where()); err == nil {
				s.User = cur.User
			}
			if user != "" {
				if !remoteaccess.ValidUser(user) {
					return errors.New("--user is not a login name")
				}
				s.User = user
			}
			if err := remoteaccess.Save(where(), s); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "remote access: "+remoteaccess.Status{State: s}.Describe())
			fmt.Fprintln(out, dirLine(where(), running))
			if remoteaccess.Effective(where()).Forced {
				fmt.Fprintln(out, "but "+remoteaccess.EnvForce+"=off is set in this environment, so it stays off")
			}
			fmt.Fprintln(out, "atrium carries the bytes. sshd decides who gets in, so keep its authorized_keys as tight as ever.")
			return nil
		},
	}
	on.Flags().StringVar(&from, "from", "*", "who may open it: hub, room names, or * for the hub and every proven room")
	on.Flags().IntVar(&port, "port", remoteaccess.DefaultPort, "the port of sshd on 127.0.0.1")
	on.Flags().StringVar(&addr, "addr", "", "where sshd answers on this machine, an IP address or a host name (default 127.0.0.1)")

	on.Flags().StringVar(&user, "user", "", "the account other rooms log in as on this machine (default: the last one set, then claude)")

	off := &cobra.Command{
		Use:   "off",
		Short: "Stop carrying ssh to this machine, and end every open stream",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cur, err := remoteaccess.Load(where())
			if err != nil {
				// A broken file is replaced by a plain off rather than left to say on.
				cur = remoteaccess.State{}
			}
			cur.On = false
			cur.SetAt, cur.SetBy = "", remoteWho()
			if err := remoteaccess.Save(where(), cur); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "remote access: off. open streams end within ten seconds")
			fmt.Fprintln(cmd.OutOrStdout(), dirLine(where(), running))
			return nil
		},
	}

	var n int
	logc := &cobra.Command{
		Use:   "log",
		Short: "This room's own record of streams, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			lines := remoteaccess.RecentLog(where(), n)
			if len(lines) == 0 {
				fmt.Fprintln(out, "no streams recorded")
				return nil
			}
			for _, l := range lines {
				fmt.Fprintln(out, formatStreamLine(l))
			}
			return nil
		},
	}
	logc.Flags().IntVar(&n, "n", 50, "how many lines")

	c.AddCommand(on, off, logc)
	return c
}

// pickSSHDAddr is the address to dial when --addr was not given. Loopback stays the answer when it is this machine's own
// sshd. When it is not (wslrelay holds 127.0.0.1:22 on a Windows machine with WSL), the door would open onto WSL, so
// this says so and picks the address that is this machine's. "" means keep the default and say nothing more.
func pickSSHDAddr(out io.Writer, port int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	f := sshwizard.Which(ctx, sshwizard.OSEnv{}, port)
	if len(f.Local) == 0 {
		return ""
	}
	if f.Chosen == remoteaccess.DefaultAddr {
		return ""
	}
	for _, n := range f.Notes {
		if strings.Contains(n, "127.0.0.1:") && !strings.Contains(n, "is this machine's sshd") && !strings.Contains(n, "refused") {
			fmt.Fprintln(out, n)
		}
	}
	if f.Chosen != "" {
		fmt.Fprintln(out, "so the door dials "+f.Chosen+", which is this machine's own sshd. override with --addr")
		return f.Chosen
	}
	fmt.Fprintln(out, "no address of this machine answers as its own sshd. run: atrium ssh setup --check")
	return ""
}

// formatStreamLine is one record as a person reads it.
func formatStreamLine(l remoteaccess.LogLine) string {
	at := l.Start
	if l.Event != "open" {
		if !l.End.IsZero() {
			at = l.End
		}
	}
	when := ""
	if !at.IsZero() {
		when = at.Local().Format("2006-01-02 15:04:05") + "  "
	}
	via := ""
	if l.Via != "" {
		via = " over " + l.Via
	}
	switch l.Event {
	case "open":
		return fmt.Sprintf("%s%s open     %s%s", when, l.ID, l.Caller, via)
	case "close":
		return fmt.Sprintf("%s%s close    %s%s, %s in, %s out, ended by %s", when, l.ID, l.Caller, via,
			streamBytes(l.BytesIn), streamBytes(l.BytesOut), orDefaultWord(l.EndedBy, "unknown"))
	}
	return fmt.Sprintf("%s%s refused  %s: %s", when, l.ID, l.Caller, l.Error)
}

func streamBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// remoteWho is user@host for set_by.
func remoteWho() string {
	name := "unknown"
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
		if i := strings.LastIndex(name, `\`); i >= 0 {
			name = name[i+1:]
		}
	}
	return name + "@" + hostname()
}

// openStreams asks the running room how many streams it has open. Not asking is fine: a stopped room has none.
func openStreams() (int, bool) {
	cl := &http.Client{Timeout: time.Second}
	resp, err := cl.Get(hubAddress("") + "/stream/status")
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, false
	}
	var v struct {
		Open int `json:"open"`
	}
	if json.NewDecoder(resp.Body).Decode(&v) != nil {
		return 0, false
	}
	return v.Open, true
}
