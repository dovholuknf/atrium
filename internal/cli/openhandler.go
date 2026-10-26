package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/api"
)

// `atrium open-handler`: the program a browser starts for an `atrium-open:` link, and the one-time install of it.
// Why a link and not a path: see internal/api/openlink.go.

func newOpenHandler() *cobra.Command {
	c := &cobra.Command{
		Use:   "open-handler",
		Short: "Open a card's file with YOUR default app, from the board's open externally button.",
		Long: "Run `atrium open-handler install` once, as the user whose desktop you sit at. It registers the " +
			"atrium-open: link under that user's own registry, so the board's open externally button, in a " +
			"browser on the room's machine, opens the file with that user's default app. The first time, the " +
			"browser asks whether to open atrium.",
	}
	c.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Register the atrium-open: link for this user.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			if err := registerOpenHandler(exe); err != nil {
				return err
			}
			fmt.Printf("registered atrium-open: for this user, handled by %s\n", filepath.ToSlash(exe))
			fmt.Println("run this again if that file moves")
			return nil
		},
	}, &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the atrium-open: link for this user.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := unregisterOpenHandler(); err != nil {
				return err
			}
			fmt.Println("removed atrium-open: for this user")
			return nil
		},
	}, &cobra.Command{
		Use:    "open <link>",
		Short:  "Handle one atrium-open: link. Started by the browser.",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			err := openFromLink(args[0])
			if err != nil {
				noteOpenFailure(err)
			}
			return err
		},
	})
	return c
}

// openFromLink redeems the link with the room it names, then opens what the room says. The room judged the path,
// and the extension is judged again here, so a room that answered wrongly still cannot start a program.
func openFromLink(raw string) error {
	addr, card, token, err := api.ParseOpenLink(raw)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"t": token})
	u := "http://" + addr + "/v1/tasks/" + url.PathEscape(card) + "/files/launch-link/redeem"
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Post(u, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("no room answered at %s: %w", addr, err)
	}
	defer resp.Body.Close()
	var out struct {
		Path  string `json:"path"`
		Error string `json:"error"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<16)).Decode(&out)
	if resp.StatusCode != http.StatusOK || out.Path == "" {
		return fmt.Errorf("the room refused: %s", out.Error)
	}
	if _, err := os.Stat(out.Path); err != nil {
		return fmt.Errorf("%s is not on this machine, so it cannot be opened here", filepath.ToSlash(out.Path))
	}
	return api.OpenWithDefault(out.Path)
}

// noteOpenFailure keeps the reason where a person can find it, because a browser starts this with no console
// that stays open.
func noteOpenFailure(err error) {
	fmt.Fprintln(os.Stderr, "atrium open-handler:", err)
	dir, derr := os.UserCacheDir()
	if derr != nil {
		return
	}
	dir = filepath.Join(dir, "atrium")
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	f, ferr := os.OpenFile(filepath.Join(dir, "open-handler.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if ferr != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %v\n", time.Now().Format(time.RFC3339), err)
}
