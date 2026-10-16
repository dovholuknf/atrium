package hubstore

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// THE DOC-DEBT METER. See docs/rnd/doc-debt-from-features.md sections 2.5, 2.8 and 6. Computed on every ask from the open
// `docs` backlog items and the feature steps, and stored nowhere, so there is no migration. The two thresholds are hub
// settings in the same key-value table as every other.
const (
	// SettingDocDebtItems is the number of open doc items at which the meter is crossed by count alone.
	SettingDocDebtItems = "doc_debt.items"
	// SettingDocDebtDays is the age of the oldest counted item at which the meter is crossed once DocDebtFewItems exist.
	SettingDocDebtDays = "doc_debt.days"

	DefaultDocDebtItems = 8
	DefaultDocDebtDays  = 10
	// DocDebtFewItems is the least number of items the age rule applies to: one forgotten item is not a backlog.
	DocDebtFewItems = 3
)

func (s *Store) docDebtNumber(name string, def int) (int, error) {
	v, err := s.HubSetting(name)
	if err != nil {
		return def, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return def, nil
	}
	return n, nil
}

// DocDebtThresholds reads the two settings, each its default when never written or not a positive number.
func (s *Store) DocDebtThresholds() (items, days int, err error) {
	if items, err = s.docDebtNumber(SettingDocDebtItems, DefaultDocDebtItems); err != nil {
		return DefaultDocDebtItems, DefaultDocDebtDays, err
	}
	if days, err = s.docDebtNumber(SettingDocDebtDays, DefaultDocDebtDays); err != nil {
		return items, DefaultDocDebtDays, err
	}
	return items, days, nil
}

// SetDocDebtThresholds writes either one, a zero leaving it as it was.
func (s *Store) SetDocDebtThresholds(items, days int) error {
	if items < 0 || days < 0 {
		return errors.New("doc_debt.items and doc_debt.days are positive numbers")
	}
	if items > 0 {
		if err := s.SetHubSetting(SettingDocDebtItems, strconv.Itoa(items)); err != nil {
			return err
		}
	}
	if days > 0 {
		return s.SetHubSetting(SettingDocDebtDays, strconv.Itoa(days))
	}
	return nil
}

// The sources of a counted item's age.
const (
	DebtFromLanded = "landed"
	DebtFromFiling = "filing"
)

// DocDebtCounted is one doc item that counts toward the meter.
type DocDebtCounted struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Feature string `json:"feature,omitempty"`
	// Since is when the debt began, and From says which clock that is: the feature's `landed` step, or the filing of an
	// item whose feature has no work row.
	Since string   `json:"since"`
	From  string   `json:"from"`
	Days  int      `json:"days"`
	Pages []string `json:"pages,omitempty"`
	// LandedSHA is the commit the feature's `landed` step recorded, empty for an item counted from filing. It is where
	// the page-changed hint counts from.
	LandedSHA string `json:"landed_sha,omitempty"`
	// Changed is the pages that changed in the hub's mirror since LandedSHA, one per page, in page order. The store never
	// fills it: the hub does (internal/link/docdebt.go), and leaves a page out when it could not tell. Nothing closes.
	Changed []DocDebtPageChange `json:"changed,omitempty"`
}

// DocDebtPageChange is the newest commit that touched one stale page since the feature landed.
type DocDebtPageChange struct {
	Path string `json:"path"`
	SHA  string `json:"sha"`
	Date string `json:"date"`
}

// DocDebtMeter is the answer to GET /_hub/doc-debt.
type DocDebtMeter struct {
	Items int `json:"items"`
	Pages int `json:"pages"`
	// Oldest is the age in days of the oldest counted item, 0 when none counts.
	Oldest int `json:"oldest_days"`
	// Pending is open doc items whose feature has not landed. They do not count, and a dropped feature's never do.
	Pending        int              `json:"pending"`
	Crossed        bool             `json:"crossed"`
	ThresholdItems int              `json:"threshold_items"`
	ThresholdDays  int              `json:"threshold_days"`
	Counted        []DocDebtCounted `json:"counted"`
}

// DocDebtMeterAt reads the open `docs` items and says what counts at `now`. An item counts from its feature's `landed`
// step, and from filing when the feature has no work row (filed by hand). One whose feature has not landed is pending,
// and one whose feature was dropped is never counted. Crossed is the count alone at the items threshold, or at least
// DocDebtFewItems items with the oldest at the days threshold.
func (s *Store) DocDebtMeterAt(now time.Time) (DocDebtMeter, error) {
	m := DocDebtMeter{Counted: []DocDebtCounted{}}
	var err error
	if m.ThresholdItems, m.ThresholdDays, err = s.DocDebtThresholds(); err != nil {
		return m, err
	}
	items, err := s.ItemList(ItemFilter{Dept: "docs", Open: true})
	if err != nil {
		return m, err
	}
	var blocks []DocDebt
	var landedSHA string
	for _, it := range items {
		landedSHA = ""
		// A body that is not a block still counts, from filing: a doc item filed by hand is still owed.
		blk, perr := ParseDocDebt(it.Body)
		since, from := it.CreatedAt, DebtFromFiling
		if perr == nil {
			feat, ferr := s.ItemGet(blk.Feature)
			if ferr == nil && feat.Status == "dropped" {
				continue
			}
			if _, werr := s.WorkByID(blk.Feature); werr == nil {
				landed, sha, ok, serr := s.landedAt(blk.Feature)
				if serr != nil {
					return m, serr
				}
				if !ok {
					m.Pending++
					continue
				}
				since, from, landedSHA = landed, DebtFromLanded, sha
			} else if !errors.Is(werr, ErrNoWork) {
				return m, werr
			}
			blocks = append(blocks, blk)
		}
		c := DocDebtCounted{ID: it.ID, Title: it.Title, Feature: blk.Feature, Since: since, From: from, LandedSHA: landedSHA}
		if t, terr := time.Parse(TimeFormat, since); terr == nil && now.After(t) {
			c.Days = int(now.Sub(t) / (24 * time.Hour))
		}
		for _, st := range blk.Stale {
			c.Pages = append(c.Pages, st.Path)
		}
		if c.Days > m.Oldest {
			m.Oldest = c.Days
		}
		m.Counted = append(m.Counted, c)
	}
	m.Items = len(m.Counted)
	m.Pages = StalePages(blocks)
	m.Crossed = m.Items >= m.ThresholdItems || (m.Items >= DocDebtFewItems && m.Oldest >= m.ThresholdDays)
	return m, nil
}

// landedAt is when an item's `landed` step finished, the sha it recorded, and whether it has.
func (s *Store) landedAt(id string) (at, sha string, ok bool, err error) {
	steps, err := s.WorkSteps(id)
	if err != nil {
		return "", "", false, err
	}
	for _, st := range steps {
		if st.Step == StepLanded && st.State == StepDone {
			if st.EndedAt != "" {
				return st.EndedAt, st.SHA, true, nil
			}
			return st.StartedAt, st.SHA, st.StartedAt != "", nil
		}
	}
	return "", "", false, nil
}

// DocDebtMeter is DocDebtMeterAt for now.
func (s *Store) DocDebtMeter() (DocDebtMeter, error) { return s.DocDebtMeterAt(s.now()) }
