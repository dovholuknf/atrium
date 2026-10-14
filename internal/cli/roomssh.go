package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dovholuknf/atrium/internal/link"
	"github.com/dovholuknf/atrium/internal/remoteaccess"
	"github.com/dovholuknf/atrium/internal/sshwizard"
)

// roomSSHCmd is `atrium room ssh`, the room's own side of turning ssh on. See docs/rnd/room-ssh-wizard-design.md.
//
// ONLY `setup --check` EXISTS, AND IT CHANGES NOTHING. It runs the wizard's checks and prints what each found. The
// fixes, the key and the switch are later steps, so without --check this refuses rather than do half of it.
func roomSSHCmd() *cobra.Command {
	c := &cobra.Command{Use: "ssh", Short: "Turn ssh on for this room, step by step"}
	c.AddCommand(sshSetupCmd())
	return c
}

// sshSetupCmd is shared by `atrium room ssh setup` and `atrium ssh setup`.
func sshSetupCmd() *cobra.Command {
	var dir, user, from string
	var port int
	var check, asJSON, noSelf bool
	c := &cobra.Command{
		Use:   "setup",
		Short: "Check what turning ssh on for this room needs, and change nothing",
		Long: "Runs the eleven steps of the ssh wizard as checks. Each one only reads this machine and says what it\n" +
			"found, and when a person must act it prints the exact command. Nothing is written: not a key, not\n" +
			"authorized_keys, not ssh config and not this room's switch.\n\n" +
			"Exits non-zero when a step failed, and zero otherwise, so a script reads the steps with --json.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !check {
				return errors.New("only --check is built so far: it prints what each step finds and changes nothing")
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
			rep := sshwizard.Check(ctx, localSSHMachine(orDefault(dir, roomDir())), opt)
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(rep); err != nil {
					return err
				}
			} else {
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
	c.Flags().BoolVar(&check, "check", false, "read only: print each step and what it found")
	c.Flags().BoolVar(&asJSON, "json", false, "print the steps as JSON")
	c.Flags().IntVar(&port, "port", 0, "the port of sshd (default: the room's switch, then 22)")
	c.Flags().StringVar(&user, "user", "", "the login other rooms use (default: the account this room runs as)")
	c.Flags().StringVar(&from, "from", "", "who may open this room's sshd: hub, room names, or * (default: the room's switch)")
	c.Flags().BoolVar(&noSelf, "no-self-test", false, "leave out this room's own key and the log in to itself")
	return c
}

// localSSHMachine is this machine and this room's key directory. The hub's ssh record does not exist yet, so the
// steps that read it say so and are skipped.
func localSSHMachine(dir string) sshwizard.LocalMachine {
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
	}
}

// sshWizardHandler is `GET /v1/ssh/wizard` on the room: the same checks, read only, as JSON. The query takes the
// same choices as the flags: port, user and from, and self_test=1 to run the self test, which is off here.
func sshWizardHandler(dir string) func(r *http.Request) any {
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
		return sshwizard.Check(r.Context(), localSSHMachine(dir), opt)
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
