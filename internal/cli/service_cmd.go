package cli

import (
	"github.com/spf13/cobra"
)

// serviceOpts is what `atrium service install` registers.
type serviceOpts struct {
	name, exe, user, passwordFile, start string
	roomArgs                             []string
}

func newServiceCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "service",
		Short: "Run a room as a Windows service: install, uninstall, status, password.",
		Long: "A room as a Windows service you control with `net stop <name>` and `net start <name>`.\n\n" +
			"Stopping the service is the room's normal graceful stop, so runners the pty host holds stay up, " +
			"exactly as on `atrium stop`. Running `atrium room` from a console is unchanged.",
	}
	var o serviceOpts
	install := &cobra.Command{
		Use:   "install --name <service> [flags] [-- <room flags>]",
		Short: "Register a room as a service. Needs an administrator.",
		Long: "Registers the room as a service that runs as --user. What follows -- is what `atrium room` takes " +
			"(--dir, --http, --agent, --db, --isolated).\n\n" +
			"START TYPE. auto starts it when the machine boots, before anybody logs in, and is the default. " +
			"manual starts it only when you run `net start <name>`. Change it later with " +
			"`sc config <name> start= demand` (manual) or `sc config <name> start= auto`.\n\n" +
			"The account needs the right to log on as a service. Install grants it. The password comes from " +
			"--password-file, else $ATRIUM_SERVICE_PASSWORD, else a prompt.",
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			o.roomArgs = args
			return installService(o)
		},
	}
	install.Flags().StringVar(&o.name, "name", "", "the service name, as net start takes it")
	install.Flags().StringVar(&o.user, "user", "", `the account to run as, as .\name or DOMAIN\name (default: the current user)`)
	install.Flags().StringVar(&o.passwordFile, "password-file", "", "a file holding the account's password")
	install.Flags().StringVar(&o.start, "start", "", "auto (at boot) or manual. Unset asks when there is a terminal, else auto")
	install.Flags().StringVar(&o.exe, "exe", "", "the atrium binary the service runs (default: this one)")
	_ = install.MarkFlagRequired("name")

	var name string
	uninstall := &cobra.Command{
		Use:   "uninstall --name <service>",
		Short: "Stop and remove the service. Needs an administrator.",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return uninstallService(name) },
	}
	uninstall.Flags().StringVar(&name, "name", "", "the service name")
	_ = uninstall.MarkFlagRequired("name")
	status := &cobra.Command{
		Use:   "status --name <service>",
		Short: "Say whether the service is installed, its state, start type and account.",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return statusService(name) },
	}
	status.Flags().StringVar(&name, "name", "", "the service name")
	_ = status.MarkFlagRequired("name")
	var passwordFile string
	password := &cobra.Command{
		Use:   "password --name <service> [--password-file <file>]",
		Short: "Store the account's new password on the service. Needs an administrator.",
		Long: "Run this when the service account's password changes, else the service fails to log on. " +
			"The password comes from --password-file, else $ATRIUM_SERVICE_PASSWORD, else a prompt.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return passwordService(name, passwordFile) },
	}
	password.Flags().StringVar(&name, "name", "", "the service name")
	password.Flags().StringVar(&passwordFile, "password-file", "", "a file holding the account's password")
	_ = password.MarkFlagRequired("name")
	c.AddCommand(install, uninstall, status, password)
	return c
}
