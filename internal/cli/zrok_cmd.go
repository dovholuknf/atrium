package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	linktransport "github.com/dovholuknf/atrium/internal/link/transport"
	"github.com/openziti/zrok/v2/environment/env_core"
	"github.com/spf13/cobra"
)

// newZrokCmd is `atrium zrok enable|disable`: a zrok environment made and removed inside the binary, with no `zrok`
// executable. --env names a directory of its own, so a test or a second account never touches ~/.zrok2.
func newZrokCmd() *cobra.Command {
	var env string
	c := &cobra.Command{Use: "zrok", Short: "Enable or disable a zrok environment, with no zrok executable"}
	c.PersistentFlags().StringVar(&env, "env", "", "the zrok environment directory (default: this machine's ~/.zrok2)")

	var tokenFile, endpoint string
	enable := &cobra.Command{
		Use:   "enable",
		Short: "Turn a zrok account token into an environment. The token is read from --token-file or stdin",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			token, err := zrokTokenFrom(tokenFile)
			if err != nil {
				return err
			}
			root, err := linktransport.LoadZrokRoot(env)
			if err != nil {
				return err
			}
			if endpoint = strings.TrimSpace(endpoint); endpoint != "" && !root.IsEnabled() {
				cfg := root.Config()
				if cfg == nil {
					cfg = &env_core.Config{}
				}
				cfg.ApiEndpoint = endpoint
				if err := root.SetConfig(cfg); err != nil {
					return err
				}
			}
			host, _ := os.Hostname()
			api, err := linktransport.EnableZrokRoot(root, token, "atrium; "+host, "atrium@"+host)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "enabled against "+api)
			return nil
		},
	}
	enable.Flags().StringVar(&tokenFile, "token-file", "", "a file holding the account token (default: read stdin)")
	enable.Flags().StringVar(&endpoint, "endpoint", "", "the zrok api to enable against (default: zrok's own)")

	disable := &cobra.Command{
		Use:   "disable",
		Short: "Remove the environment from the account and from disk",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := linktransport.LoadZrokRoot(env)
			if err != nil {
				return err
			}
			refused, err := linktransport.DisableZrokRoot(root)
			if err != nil {
				return err
			}
			if refused != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "removed here. the account would not remove it: "+refused.Error())
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "disabled")
			return nil
		},
	}
	c.AddCommand(enable, disable)
	return c
}

func zrokTokenFrom(file string) (string, error) {
	if file != "" {
		raw, err := os.ReadFile(file)
		return strings.TrimSpace(string(raw)), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.TrimSpace(line) != "" {
		return strings.TrimSpace(line), nil
	}
	return "", fmt.Errorf("no token on stdin: %v", err)
}
