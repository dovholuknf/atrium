package hubstore

import "testing"

const impactBlock = `feature: u-099
changelog: changelog/ui/2026-10-15-u-099.md
shas: abc1234
new: flag --pin on atrium launch
new: a Pin button on the card
stale: README.md  still says atrium daemon
stale: website/docs/cli.md
updated: website/docs/quick-start.md docs/x.md
a note the worker left
`

func TestDocsDepartmentHasThePrefixD(t *testing.T) {
	if p, ok := DeptPrefix("docs"); !ok || p != "d" {
		t.Fatalf("docs prefix = %q, %v", p, ok)
	}
}

func TestParseDocDebtReadsTheBlock(t *testing.T) {
	d, err := ParseDocDebt(impactBlock)
	if err != nil {
		t.Fatal(err)
	}
	if d.Feature != "u-099" || d.Changelog != "changelog/ui/2026-10-15-u-099.md" || d.Shas != "abc1234" {
		t.Errorf("header: %+v", d)
	}
	if len(d.New) != 2 || len(d.Stale) != 2 || len(d.Updated) != 2 {
		t.Fatalf("counts: %+v", d)
	}
	if d.Stale[0].Path != "README.md" || d.Stale[0].Why != "still says atrium daemon" || d.Stale[1].Why != "" {
		t.Errorf("stale: %+v", d.Stale)
	}
	if got := StalePages([]DocDebt{d, d}); got != 2 {
		t.Errorf("pages = %d, want 2", got)
	}
}

func TestParseDocDebtRefusesABlockWithNoDebt(t *testing.T) {
	for _, body := range []string{"", "feature: u-1\nnew: x", "stale: README.md"} {
		if _, err := ParseDocDebt(body); err == nil {
			t.Errorf("%q parsed", body)
		}
	}
}
