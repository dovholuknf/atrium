package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/base/sshcfg"
	"github.com/dovholuknf/atrium/internal/link"
)

// sshConfigCmd is `atrium ssh config add|rm|show`: the opt-in helper that writes the Host blocks `atrium ssh-proxy
// --print-config` prints. See docs/rnd/ssh-via-hub.md.
//
// THE ONE EXCEPTION to atrium never editing ~/.ssh/config, and a narrow one. It writes its own file,
// ~/.ssh/atrium_config, and adds one `Include atrium_config` line at the top of ~/.ssh/config when that is missing,
// after a timestamped backup. Nothing else in that file is touched, and `rm` never removes the Include. It is
// not run by anything but a person typing it.
func sshConfigCmd() *cobra.Command {
	var hub string
	home := func() (string, error) {
		h, err := os.UserHomeDir()
		if err != nil || h == "" {
			return "", errors.New("could not find this account's home directory")
		}
		return h, nil
	}
	c := &cobra.Command{
		Use:   "config",
		Short: "Write the ssh Host blocks that reach rooms through the hub, into ~/.ssh/atrium_config",
		Long: "Writes the blocks `atrium ssh-proxy --print-config` prints, for this machine's OS, into\n" +
			"~/.ssh/atrium_config (%USERPROFILE%\\.ssh\\atrium_config on Windows), and adds one line,\n" +
			"`Include atrium_config`, at the top of ~/.ssh/config if it is missing, after a timestamped backup.\n" +
			"It touches nothing else in that file, and remove never takes the Include line out.",
	}

	var all bool
	add := &cobra.Command{
		Use:   "add <room>... | --all",
		Short: "Write the Host block for each room",
		RunE: func(cmd *cobra.Command, args []string) error {
			rooms, err := roomsToConfigure(args, all, hub, true)
			if err != nil {
				return err
			}
			h, err := home()
			if err != nil {
				return err
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			users := roomLogins(cmd.Context(), hub)
			res, err := sshcfg.AddHubUsers(h, exe, runtime.GOOS, hubForConfig(hub), rooms, users)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			var noUser []string
			for _, r := range rooms {
				if users[r] == "" {
					noUser = append(noUser, r)
				}
			}
			_, cfg, own := sshcfg.Paths(h)
			if !res.Changed {
				fmt.Fprintf(out, "nothing to change: %s already has %s\n", own, strings.Join(rooms, ", "))
				return nil
			}
			fmt.Fprintf(out, "wrote %s for %s\n", own, strings.Join(rooms, ", "))
			switch {
			case res.Created:
				fmt.Fprintf(out, "created %s with the line: %s\n", cfg, sshcfg.IncludeLine)
			case res.Include:
				fmt.Fprintf(out, "added to the top of %s: %s\n", cfg, sshcfg.IncludeLine)
				fmt.Fprintf(out, "backed it up first as %s\n", res.Backup)
			}
			if users[rooms[0]] != "" {
				fmt.Fprintf(out, "try it: ssh %s   (logs in as %s, the login that room published)\n", rooms[0], users[rooms[0]])
			} else {
				fmt.Fprintf(out, "try it: ssh <user>@%s\n", rooms[0])
			}
			if len(noUser) > 0 {
				fmt.Fprintf(out, "no login user is known for %s: the hub has none from them yet, so no User line was written\n",
					strings.Join(noUser, ", "))
			}
			return nil
		},
	}
	add.Flags().BoolVar(&all, "all", false, "every room this machine's hub lists (the hub's machine only)")
	add.Flags().StringVar(&hub, "hub", "", "the hub's loopback board, when it is not the recorded or default one")

	var rmAll bool
	rm := &cobra.Command{
		Use:   "rm <room>... | --all",
		Short: "Remove the Host block for each room (never the Include line)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !rmAll && len(args) == 0 {
				return errors.New("name the rooms to remove, or --all")
			}
			if rmAll && len(args) > 0 {
				return errors.New("name the rooms or use --all, not both")
			}
			h, err := home()
			if err != nil {
				return err
			}
			removed, missing, err := sshcfg.Remove(h, args)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(removed) > 0 {
				fmt.Fprintf(out, "removed %s from %s\n", strings.Join(removed, ", "), pathOwn(h))
			}
			if len(missing) > 0 {
				fmt.Fprintf(out, "not there: %s\n", strings.Join(missing, ", "))
			}
			if len(removed) == 0 && len(missing) == 0 {
				fmt.Fprintln(out, "nothing configured")
			}
			return nil
		},
	}
	rm.Flags().BoolVar(&rmAll, "all", false, "every block atrium wrote")

	show := &cobra.Command{
		Use:   "show",
		Short: "What is configured, and whether ~/.ssh/config includes it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			h, err := home()
			if err != nil {
				return err
			}
			s, err := sshcfg.Show(h)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			_, cfg, own := sshcfg.Paths(h)
			if len(s.Rooms) == 0 {
				fmt.Fprintf(out, "no rooms configured in %s\n", own)
			} else {
				fmt.Fprintf(out, "%s has: %s\n\n%s\n", own, strings.Join(s.Rooms, ", "), s.Text)
			}
			switch {
			case !s.ConfigExists:
				fmt.Fprintf(out, "%s does not exist, so ssh does not read atrium_config yet\n", cfg)
			case s.Included:
				fmt.Fprintf(out, "%s includes it\n", cfg)
			default:
				fmt.Fprintf(out, "%s does not include it yet. atrium ssh config add adds the line: %s\n", cfg, sshcfg.IncludeLine)
			}
			return nil
		},
	}
	c.AddCommand(add, rm, show)
	return c
}

// roomLogins is the login user each room published, by room name. Best effort and empty when nothing can say: a room
// asks the hub over its own link, and a machine with no room asks the hub's board on loopback.
func roomLogins(ctx context.Context, hub string) map[string]string {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var set []link.SSHEntry
	if room, d, err := roomLinkFor(""); err == nil {
		defer closeDialer(d)
		set, _ = link.FetchSSHSet(ctx, d, room)
	}
	if len(set) == 0 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, boardAddress(hub)+"/_hub/sshkeys", nil)
		if err == nil {
			if resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req); err == nil {
				defer resp.Body.Close()
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
				var v struct {
					Keys []link.SSHEntry `json:"keys"`
				}
				if resp.StatusCode == http.StatusOK && json.Unmarshal(b, &v) == nil {
					set = v.Keys
				}
			}
		}
	}
	out := map[string]string{}
	for _, e := range set {
		if e.User != "" {
			out[e.Room] = e.User
		}
	}
	return out
}

func pathOwn(home string) string {
	_, _, own := sshcfg.Paths(home)
	return own
}

// hubForConfig is the hub board to write into a ProxyCommand, or "" when the hub is on its default board and the proxy
// finds it unaided. An explicit --hub is written as given. Otherwise the boards the proxy would try are asked in its
// order, and the first that answers as a hub is the one found.
func hubForConfig(flag string) string {
	if flag != "" {
		if b := boardAddress(flag); !sameBoard(b, defaultHubBoard) {
			return b
		}
		return ""
	}
	first, alt := hubDoors("")
	cl := &http.Client{Timeout: 3 * time.Second}
	for _, b := range []string{first, alt} {
		if b == "" {
			continue
		}
		resp, err := cl.Get(b + "/_hub/rooms")
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		if sameBoard(b, defaultHubBoard) {
			return ""
		}
		return b
	}
	return ""
}

// roomsToConfigure is the rooms named, or every room the hub on this machine lists.
func roomsToConfigure(args []string, all bool, hub string, need bool) ([]string, error) {
	if all && len(args) > 0 {
		return nil, errors.New("name the rooms or use --all, not both")
	}
	if !all {
		if len(args) == 0 && need {
			return nil, errors.New("name the rooms, or use --all on the machine that runs the hub")
		}
		return args, nil
	}
	cl := &http.Client{Timeout: 3 * time.Second}
	resp, err := cl.Get(boardAddress(hub) + "/_hub/rooms")
	if err != nil {
		return nil, errors.New("--all needs the hub on this machine, and none answers here. name the rooms instead: " +
			"atrium ssh config add sg3 m1mini")
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var v struct {
		Rooms []struct {
			Name string `json:"name"`
		} `json:"rooms"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(b, &v) != nil {
		return nil, errors.New("--all needs the hub on this machine, and what answers here is not the hub. " +
			"name the rooms instead: atrium ssh config add sg3 m1mini")
	}
	var names []string
	for _, r := range v.Rooms {
		if r.Name != "" {
			names = append(names, r.Name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, errors.New("the hub lists no rooms")
	}
	return names, nil
}
