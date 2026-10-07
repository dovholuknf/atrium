// Package recognisers is the seed for the hub's recogniser table: the rows in this directory, built into the binary.
//
// The hub seeds its table from these once, so a hub and its rooms recognise a pasted link with nothing loaded by
// hand. load.ps1 stays for loading a file of your own. See docs/rnd/card-lifecycle-design.md, section 2.
package recognisers

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/dovholuknf/atrium/internal/store"
)

//go:embed *.json
var files embed.FS

// Seed is every row in the JSON files of this directory, checked, in rank order.
func Seed() ([]store.Recogniser, error) {
	names, err := files.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var out []store.Recogniser
	seen := map[string]string{}
	for _, f := range names {
		raw, err := files.ReadFile(f.Name())
		if err != nil {
			return nil, err
		}
		var rows []store.Recogniser
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name(), err)
		}
		for _, r := range rows {
			r, err := store.CheckRecogniser(r)
			if err != nil {
				return nil, fmt.Errorf("%s: %s: %w", f.Name(), r.ID, err)
			}
			if prev, dup := seen[r.ID]; dup {
				return nil, fmt.Errorf("%s: %s is also in %s", f.Name(), r.ID, prev)
			}
			seen[r.ID] = f.Name()
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Rank != out[j].Rank {
			return out[i].Rank < out[j].Rank
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
