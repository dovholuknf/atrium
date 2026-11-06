package daemon

import "testing"

func TestUsagePriceFable5(t *testing.T) {
	t.Parallel()
	p, ok := usagePriceFor("claude-fable-5")
	if !ok || p.In != 10 || p.Cw5m != 12.5 || p.Cw1h != 20 || p.Cr != 1 || p.Out != 50 {
		t.Fatalf("fable 5: %+v %v", p, ok)
	}
	if p, _ := usagePriceFor("claude-fable-5-1"); p.Cr != 0.25 {
		t.Fatalf("fable 5.1 taken for fable 5: %+v", p)
	}
}

func TestUsagePriceHaiku55TierBoundary(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		model  string
		prompt int64
		in, cr float64
	}{
		{"claude-haiku-5-5", 0, 0.10, 0.01},
		{"claude-haiku-5-5", 100_000, 0.10, 0.01},
		{"claude-haiku-5-5", 100_001, 0.50, 0.05},
		{"claude-haiku-5-5-20260101", 100_001, 0.50, 0.05},
	} {
		p, ok := usagePriceForPrompt(c.model, c.prompt)
		if !ok || p.In != c.in || p.Cr != c.cr {
			t.Fatalf("%s at %d: %+v %v", c.model, c.prompt, p, ok)
		}
	}
	if p, _ := usagePriceForPrompt("claude-haiku-5-5", 100_001); p.Cw5m != 0.625 || p.Cw1h != 1 || p.Out != 2.50 {
		t.Fatalf("upper tier: %+v", p)
	}
	if p, _ := usagePriceForPrompt("claude-sonnet-5-5", 500_000); p.In != 2 {
		t.Fatalf("a flat model changed tier: %+v", p)
	}
}
