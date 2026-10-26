package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/link"
)

// roomsTable is `atrium rooms` with no verb: one row per room the running hub knows, offline ones included.
func roomsTable(cmd *cobra.Command, hub string) error {
	cl := &http.Client{Timeout: 5 * time.Second}
	resp, err := cl.Get(boardAddress(hub) + "/_hub/inventory")
	if err != nil {
		return fmt.Errorf("no hub answers at %s: %w", boardAddress(hub), err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var inv struct {
		Rooms []link.Known `json:"rooms"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(b, &inv) != nil {
		return fmt.Errorf("%s did not answer as a hub", boardAddress(hub))
	}
	out := cmd.OutOrStdout()
	if len(inv.Rooms) == 0 {
		fmt.Fprintln(out, "no rooms yet. `"+atriumCmd("rooms add")+" <name>` makes one")
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tHOST\tVIA\tBUILD\tCONNECTED\tLAST SEEN\tDIALS\tTRANSPORTS")
	for _, r := range inv.Rooms {
		fmt.Fprintln(w, strings.Join(roomRow(r), "\t"))
	}
	return w.Flush()
}

func roomRow(r link.Known) []string {
	dash := func(s string) string {
		if s == "" {
			return "-"
		}
		return s
	}
	connected, seen := "no", "never"
	if r.Attached {
		connected = "yes"
	}
	if r.LastSeen != nil {
		seen = r.LastSeen.Local().Format("2006-01-02 15:04:05")
	}
	var dials, kinds []string
	for _, p := range r.Paths {
		kinds = append(kinds, p.Kind)
		if p.State == link.PathUp && p.Addr != "" {
			dials = append(dials, p.Addr)
		}
	}
	return []string{r.Name, dash(r.Host), dash(r.Via), dash(r.Version), connected, seen,
		dash(strings.Join(dials, ",")), dash(strings.Join(kinds, ","))}
}
