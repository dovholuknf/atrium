package hubstore

import (
	"errors"
	"strings"
)

// The impact block a `docs` backlog item carries as its body. See docs/rnd/doc-debt-from-features.md section 2.4.
// The `stale:` lines are the machine-readable part: they give the meter its page count and the doc worker its list.

// DocDebtStale is one page a feature left wrong, and what it says wrong.
type DocDebtStale struct {
	Path string `json:"path"`
	Why  string `json:"why,omitempty"`
}

// DocDebt is a parsed impact block.
type DocDebt struct {
	Feature   string         `json:"feature"`
	Changelog string         `json:"changelog,omitempty"`
	Shas      string         `json:"shas,omitempty"`
	New       []string       `json:"new,omitempty"`
	Stale     []DocDebtStale `json:"stale,omitempty"`
	Updated   []string       `json:"updated,omitempty"`
}

// ParseDocDebt reads an impact block. Keys may repeat (`new`, `stale`), a line with no known key is skipped so a
// worker's note does not spoil it, and a block with no `feature:` or no `stale:` is refused, since a doc item with
// nothing stale is no debt and is not filed.
func ParseDocDebt(body string) (DocDebt, error) {
	var d DocDebt
	for _, line := range strings.Split(body, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "feature":
			d.Feature = val
		case "changelog":
			d.Changelog = val
		case "shas":
			d.Shas = val
		case "new":
			if val != "" {
				d.New = append(d.New, val)
			}
		case "stale":
			if val == "" {
				continue
			}
			path, why, _ := strings.Cut(val, " ")
			d.Stale = append(d.Stale, DocDebtStale{Path: path, Why: strings.TrimSpace(why)})
		case "updated":
			d.Updated = append(d.Updated, strings.Fields(val)...)
		}
	}
	if d.Feature == "" {
		return DocDebt{}, errors.New("an impact block needs a `feature:` line naming the feature's item id")
	}
	if len(d.Stale) == 0 {
		return DocDebt{}, errors.New("an impact block needs at least one `stale:` line, or no doc item is filed")
	}
	return d, nil
}

// StalePages is the distinct stale paths of a set of blocks, which is the meter's page count.
func StalePages(blocks []DocDebt) int {
	seen := map[string]bool{}
	for _, b := range blocks {
		for _, s := range b.Stale {
			seen[s.Path] = true
		}
	}
	return len(seen)
}
