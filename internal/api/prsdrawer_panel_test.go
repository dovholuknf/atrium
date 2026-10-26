package api

import "testing"

const panelSample = "# Merged findings, PR #396 @ eef14f1 (3 reviewers)\n\n" +
	"- high-01 security cmd/x.go:12 + cmd/y.go:40 -- first line\n" +
	"  second line\n" +
	"- medium-02 correctness internal/a/b.go:7 -- plain\n" +
	"- pre-existing-03 (low) internal/c.go:99 -- old\n" +
	"- not an item\n"

func TestParsePanelReadsItemsAndCountsTheSkipped(t *testing.T) {
	t.Parallel()
	items, skipped := parsePanel(panelSample)
	if len(items) != 3 || skipped != 1 {
		t.Fatalf("%d items, %d skipped: %+v", len(items), skipped, items)
	}
	a, b, c := items[0], items[1], items[2]
	if a.Sev != "high" || a.Num != 1 || a.Category != "security" || a.Path != "cmd/x.go" || a.Line != 12 ||
		a.Text != "first line\nsecond line" {
		t.Fatalf("%+v", a)
	}
	if b.Sev != "med" || b.Path != "internal/a/b.go" || b.Line != 7 {
		t.Fatalf("%+v", b)
	}
	if !c.PreExisting || c.Sev != "low" || c.Num != 3 || c.Line != 99 {
		t.Fatalf("%+v", c)
	}
}

func TestPanelFindingIsAFileTheDrawerParses(t *testing.T) {
	t.Parallel()
	items, _ := parsePanel(panelSample)
	name := panelFile(items[0], 4)
	if name != "04-high-cmd-x.go-L12.txt" {
		t.Fatalf("name %q", name)
	}
	pos, sev, ok := findingSev(name)
	if !ok || pos != 4 || sev != "high" {
		t.Fatalf("%d %q %v", pos, sev, ok)
	}
	f := parseFinding(name, pos, sev, []byte(panelText(items[0], "https://x/pull/396")))
	if f.Path != "cmd/x.go" || f.Line != 12 || f.Group != "panel" || f.Key != "f-panel-high-01" ||
		f.Comment != "first line\nsecond line" {
		t.Fatalf("%+v", f)
	}
}
