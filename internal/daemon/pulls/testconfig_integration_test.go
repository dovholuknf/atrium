//go:build integration

package pulls

import (
	"context"
	"encoding/json"
	"sort"
)

// testConfig stands in for the daemon's seams: the lean flags a run replaces, the environment merge and the price of
// the sonnet-5-5 rows the receipts here use.
func testConfig() Config {
	return Config{
		Fork:         func(ctx context.Context, spec ForkSpec) ([]byte, error) { return nil, nil },
		UserSettings: func() []byte { return nil },
		LeanArgs: func(args []string, userSettings []byte, gateway string) ([]string, error) {
			return append(args, "--strict-mcp-config", "--setting-sources", "project,local"), nil
		},
		ForkEnv: func(extra, harnessEnv map[string]string) {},
		ChildEnv: func(base []string, harnessEnv map[string]string, atrium map[string]string) []string {
			out := append([]string{}, base...)
			var keys []string
			for k := range atrium {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				out = append(out, k+"="+atrium[k])
			}
			return out
		},
		Cost: func(raw []byte) float64 {
			var r Receipt
			if json.Unmarshal(raw, &r) != nil || r.Usage == nil {
				return 0
			}
			u := r.Usage
			return (float64(u.Input)*2 + float64(u.CacheWrite)*4 + float64(u.CacheRead)*0.10 + float64(u.Output)*10) / 1e6
		},
	}
}
