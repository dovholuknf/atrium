//go:build integration

package hubstore

import (
	"fmt"
	"testing"
	"time"
)

var debtDay0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// debtStore is a store whose clock the test moves, so an age is measured and not slept through.
func debtStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	s := open(t)
	now := debtDay0
	s.clock = func() time.Time { return now }
	return s, &now
}

func debtBlock(feature string, pages ...string) string {
	b := "feature: " + feature + "\nchangelog: changelog/rnd/x.md\n"
	for _, p := range pages {
		b += "stale: " + p + " says it wrong\n"
	}
	return b
}

func fileDocItem(t *testing.T, s *Store, id, feature string, pages ...string) {
	t.Helper()
	if _, err := s.ItemFile(ItemNew{ID: id, Dept: "docs", Title: "docs behind " + feature, Body: debtBlock(feature, pages...), FiledBy: "t"}); err != nil {
		t.Fatal(err)
	}
}

// landFeature makes the feature's work row and ends its landed step at `at`.
func landFeature(t *testing.T, s *Store, id string, at time.Time) {
	t.Helper()
	if _, _, err := s.PutItem(onWork("a-"+id, "sg4", "c-"+id, id, 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutWorkStep(WorkStepRow{BacklogID: id, Step: StepLanded, State: StepDone, StartedAt: ts(at), EndedAt: ts(at), SHA: "abc"}); err != nil {
		t.Fatal(err)
	}
}

// THE LANDED SHA is carried on a counted item whose feature landed, and absent from one counted from filing, which is what
// keeps a hand-filed item out of the page-changed hint.
func TestDocDebtCarriesTheLandedSHAOfAnItemItCountsFromLanded(t *testing.T) {
	s, now := debtStore(t)
	landFeature(t, s, "f-one", debtDay0)
	fileDocItem(t, s, "d-1", "f-one", "README.md")
	fileDocItem(t, s, "d-2", "f-by-hand", "README.md")
	*now = debtDay0.Add(24 * time.Hour)
	m, err := s.DocDebtMeter()
	if err != nil || m.Items != 2 {
		t.Fatalf("meter = %+v %v", m, err)
	}
	for _, c := range m.Counted {
		switch c.ID {
		case "d-1":
			if c.LandedSHA != "abc" {
				t.Fatalf("landed item: %+v", c)
			}
		case "d-2":
			if c.LandedSHA != "" {
				t.Fatalf("hand-filed item: %+v", c)
			}
		}
	}
}

// ITEMS AND PAGES: only open docs items count, and a page two items name is one page.
func TestDocDebtCountsOpenItemsAndDistinctPages(t *testing.T) {
	s, now := debtStore(t)
	landFeature(t, s, "f-one", debtDay0)
	landFeature(t, s, "f-two", debtDay0)
	landFeature(t, s, "f-three", debtDay0)
	fileDocItem(t, s, "d-1", "f-one", "README.md", "website/docs/cli.md")
	fileDocItem(t, s, "d-2", "f-two", "README.md")
	fileDocItem(t, s, "d-3", "f-three", "FEATURES.md")
	if _, err := s.ItemSetStatus("d-3", "done", "t"); err != nil {
		t.Fatal(err)
	}
	s.ItemFile(ItemNew{ID: "r-1", Dept: "rnd", Title: "not docs", Body: debtBlock("f-one", "x.md"), FiledBy: "t"})
	*now = debtDay0.Add(2 * 24 * time.Hour)
	m, err := s.DocDebtMeter()
	if err != nil {
		t.Fatal(err)
	}
	if m.Items != 2 || m.Pages != 2 || m.Oldest != 2 || m.Crossed {
		t.Fatalf("meter = %+v", m)
	}
}

// THE AGE SOURCE: from the feature's landed step, from filing when the feature has no work row, and not at all while the
// feature has not landed or was dropped.
func TestDocDebtAgeStartsAtLandedOrFilingAndPendingDoesNotCount(t *testing.T) {
	s, now := debtStore(t)
	// filed on day 0, its feature lands on day 6: the age is 4 days on day 10, not 10
	fileDocItem(t, s, "d-1", "f-late", "a.md")
	landFeature(t, s, "f-late", debtDay0.Add(6*24*time.Hour))
	// no work row: counts from filing, day 0
	fileDocItem(t, s, "d-2", "f-byhand", "b.md")
	// a work row with no landed step is pending
	if _, _, err := s.PutItem(onWork("a-w", "sg4", "c-w", "f-wip", 1)); err != nil {
		t.Fatal(err)
	}
	fileDocItem(t, s, "d-3", "f-wip", "c.md")
	// a dropped feature never counts
	s.ItemFile(ItemNew{ID: "f-gone", Dept: "rnd", Title: "gone", FiledBy: "t"})
	s.ItemSetStatus("f-gone", "dropped", "t")
	fileDocItem(t, s, "d-4", "f-gone", "d.md")
	*now = debtDay0.Add(10 * 24 * time.Hour)

	m, err := s.DocDebtMeter()
	if err != nil {
		t.Fatal(err)
	}
	if m.Items != 2 || m.Pending != 1 || m.Oldest != 10 {
		t.Fatalf("meter = %+v", m)
	}
	from := map[string]string{}
	days := map[string]int{}
	for _, c := range m.Counted {
		from[c.ID], days[c.ID] = c.From, c.Days
	}
	if from["d-1"] != DebtFromLanded || days["d-1"] != 4 || from["d-2"] != DebtFromFiling || days["d-2"] != 10 {
		t.Fatalf("counted = %+v", m.Counted)
	}
}

// BOTH THRESHOLDS: 8 items by count, or 3 or more with the oldest at 10 days, and both are hub settings.
func TestDocDebtCrossesOnCountOrOnAge(t *testing.T) {
	s, now := debtStore(t)
	for i := 1; i <= 7; i++ {
		fileDocItem(t, s, fmt.Sprintf("d-%d", i), fmt.Sprintf("f-%d", i), "p.md")
	}
	*now = debtDay0.Add(9 * 24 * time.Hour)
	if m, _ := s.DocDebtMeter(); m.Crossed || m.ThresholdItems != 8 || m.ThresholdDays != 10 {
		t.Fatalf("7 items at 9 days: %+v", m)
	}
	*now = debtDay0.Add(10 * 24 * time.Hour)
	if m, _ := s.DocDebtMeter(); !m.Crossed {
		t.Fatalf("7 items at 10 days: %+v", m)
	}
	// two items however old are not a backlog
	s2, now2 := debtStore(t)
	fileDocItem(t, s2, "d-1", "f-1", "p.md")
	fileDocItem(t, s2, "d-2", "f-2", "p.md")
	*now2 = debtDay0.Add(30 * 24 * time.Hour)
	if m, _ := s2.DocDebtMeter(); m.Crossed {
		t.Fatalf("2 old items: %+v", m)
	}
	// the count alone, on day 0
	s3, _ := debtStore(t)
	for i := 1; i <= 8; i++ {
		fileDocItem(t, s3, fmt.Sprintf("d-%d", i), fmt.Sprintf("f-%d", i), "p.md")
	}
	if m, _ := s3.DocDebtMeter(); !m.Crossed || m.Oldest != 0 {
		t.Fatalf("8 new items: %+v", m)
	}
	// the settings move both
	if err := s3.SetDocDebtThresholds(9, 0); err != nil {
		t.Fatal(err)
	}
	if m, _ := s3.DocDebtMeter(); m.Crossed || m.ThresholdItems != 9 {
		t.Fatalf("threshold 9: %+v", m)
	}
	s3.SetHubSetting(SettingDocDebtDays, "5")
	s3.SetHubSetting(SettingDocDebtItems, "junk")
	if i, d, _ := s3.DocDebtThresholds(); i != DefaultDocDebtItems || d != 5 {
		t.Fatalf("thresholds = %d, %d", i, d)
	}
}
