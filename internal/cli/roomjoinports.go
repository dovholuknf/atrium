package cli

import (
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/spf13/cobra"
)

// joinFlagsSet is every flag the join was given apart from --http and --agent, for the rerun line to keep.
func joinFlagsSet(cmd *cobra.Command) string {
	var b strings.Builder
	for _, n := range []string{"dir", "db", "identity", "mtls", "openziti", "service", "zrok-private"} {
		if v, err := cmd.Flags().GetString(n); err == nil && cmd.Flags().Changed(n) {
			if strings.ContainsAny(v, " \t") {
				v = `"` + v + `"`
			}
			fmt.Fprintf(&b, " --%s %s", n, v)
		}
	}
	for _, n := range []string{"isolated", "accept-upgrades"} {
		if v, err := cmd.Flags().GetBool(n); err == nil && v {
			fmt.Fprintf(&b, " --%s", n)
		}
	}
	return b.String()
}

// joinPortsFree refuses a join whose room could not bind, before anything is written, naming the exact rerun.
func joinPortsFree(out io.Writer, joinString, human, agent string) error {
	busy := func(addr string) bool {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return true
		}
		_ = ln.Close()
		return false
	}
	hBusy, aBusy := busy(human), busy(agent)
	if !hBusy && !aBusy {
		return nil
	}
	var taken []string
	for _, b := range []struct {
		on   bool
		addr *string
	}{{hBusy, &human}, {aBusy, &agent}} {
		if !b.on {
			continue
		}
		taken = append(taken, *b.addr)
		port, release, err := freePort(0)
		if err != nil {
			return err
		}
		defer release()
		*b.addr = fmt.Sprintf("127.0.0.1:%d", port)
	}
	_, _ = fmt.Fprintf(out, "  %s already in use on this machine, by a hub or another room. nothing was saved. rerun as:\n"+
		"  %s %s --http %s --agent %s\n", strings.Join(taken, " and "), atriumCmd("room join"), joinString, human, agent)
	return alreadySaid("ports in use: %s", strings.Join(taken, ", "))
}
