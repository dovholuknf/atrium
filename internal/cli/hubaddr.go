package cli

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// hubAddrs is what a running hub bound, written to its directory so a command on this machine that was not told
// reaches the hub where it is rather than at the default ports.
type hubAddrs struct {
	Board     string `json:"board"`
	Link      string `json:"link"`
	Advertise string `json:"advertise,omitempty"`
}

func hubAddrFile(dir string) string { return filepath.Join(dir, "hub-addr.json") }

func writeHubAddrs(dir string, a hubAddrs) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(hubAddrFile(dir), raw, 0o600)
}

// readHubAddrs is the zero value when no hub has written one.
func readHubAddrs(dir string) hubAddrs {
	var a hubAddrs
	if raw, err := os.ReadFile(hubAddrFile(dir)); err == nil {
		_ = json.Unmarshal(raw, &a)
	}
	return a
}

// recordedBoard is the hub's board as 127.0.0.1:port, else the default.
func recordedBoard(dir string) string {
	if _, port, err := net.SplitHostPort(readHubAddrs(dir).Board); err == nil && port != "" {
		return net.JoinHostPort("127.0.0.1", port)
	}
	return defaultBoardAddr()
}

// recordedLink puts the running hub's link, and its advertise unless one was given, in place of a --link left unset.
func recordedLink(c *cobra.Command, dir string, port, advertise *string) {
	a := readHubAddrs(dir)
	if c.Flags().Changed("link") || a.Link == "" {
		return
	}
	*port = a.Link
	if !c.Flags().Changed("link-advertise") {
		*advertise = a.Advertise
	}
}
