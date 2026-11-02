//go:build integration

package forge

import (
	"strings"

	"github.com/dovholuknf/atrium/internal/base/rules"
)

// seedOf is a seed that gives exactly `rows`.
func seedOf(rows ...rules.Recogniser) SeedFunc {
	return func() ([]rules.Recogniser, error) { return append([]rules.Recogniser(nil), rows...), nil }
}

func seedRow(id string, rank int, pattern string) rules.Recogniser {
	return rules.Recogniser{ID: id, Label: id, Enabled: true, Rank: rank, Pattern: pattern, FetchArgs: []string{}}
}

func rowIDs(rows []*rules.Recogniser) string {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return strings.Join(ids, ",")
}
