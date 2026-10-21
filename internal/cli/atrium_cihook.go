package cli

import (
	"context"
	"fmt"
	"net"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/spf13/cobra"
)

// startCIHook wires the hub's CI hook: the loopback route and the delivery worker always, the ziti-hook bind only when a
// service is named. See docs/fabric/ci-hook-design.md.
func startCIHook(ctx context.Context, service string, store *hubstore.Store, ht *link.HubTransports, proxy *link.Proxy) {
	hook := link.NewCIHook(store, proxy.Deliverer(), func(kind, detail string) { proxy.RecordAudit("", kind, detail) })
	proxy.SetCIHook(hook)
	events := proxy.NewEventHook(store, "land@"+defaultRoomName(), func(kind, detail string) { proxy.RecordAudit("", kind, detail) })
	hook.SetEvents(events)
	proxy.SetEvents(events)
	go hook.Run(ctx)
	var seed link.TransportSeed
	if service != "" {
		on := true
		seed = link.TransportSeed{On: &on, Service: service}
	}
	ht.Add(link.TransportItem{
		Name: link.TransportCIHook, Label: "CI hook", Kind: link.CIListenerKind, DefaultService: "ziti-hook",
		Listen: func(svc, identity string) (net.Listener, error) {
			return (&link.Ziti{Identity: identity, Service: svc}).ListenService(svc)
		},
		Handler: hook,
	}, seed)
}

// newCIHookCmd is `atrium ci-hook`.
func newCIHookCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "ci-hook",
		Short: "The hub's CI webhook: its shared secret",
	}
	c.AddCommand(ciHookSecretCmd())
	return c
}

func ciHookSecretCmd() *cobra.Command {
	var f hubStoreFlags
	var rotate bool
	c := &cobra.Command{
		Use:   "secret",
		Short: "Mint the secret CI sends in X-Ziti-Hook-Secret, and print it once",
		Long: "Prints a new 32-byte secret, base64url, ONCE. The hub keeps only its SHA-256, so a lost secret is\n" +
			"minted again and never shown again. Run it on the machine the hub runs on: it opens the hub's store\n" +
			"directly.\n\n" +
			"--rotate mints another and keeps the previous one good for 24 hours, so changing the GitHub secret\n" +
			"is not a race.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := f.open()
			if err != nil {
				return err
			}
			defer store.Close()
			secret, err := store.MintCIHookSecret(rotate)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), secret)
			return nil
		},
	}
	f.bind(c, "atrium-")
	c.Flags().BoolVar(&rotate, "rotate", false, "replace the secret, keeping the old one good for 24 hours")
	return c
}
