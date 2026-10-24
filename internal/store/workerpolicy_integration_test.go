//go:build integration

package store

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/rules"
)

func TestWorkerPolicyIsOffUntilSet(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real sqlite database on disk")
	}
	t.Parallel()
	s := openTestStore(t)
	if p := s.WorkerPolicy(); p != (rules.WorkerPolicy{}) {
		t.Fatalf("policy %+v on a fresh store", p)
	}
}

func TestWorkerPolicyRoundTripsAndOffIsEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real sqlite database on disk")
	}
	t.Parallel()
	s := openTestStore(t)
	v, err := rules.CheckWorkerPolicy(rules.WorkerPolicy{Model: " sonnet ", BudgetUSD: 12.345})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(rules.SettingWorkerPolicy, v); err != nil {
		t.Fatal(err)
	}
	if p := s.WorkerPolicy(); p.Model != "sonnet" || p.BudgetUSD != 12.35 {
		t.Fatalf("read back %+v", p)
	}
	if off, err := rules.CheckWorkerPolicy(rules.WorkerPolicy{}); err != nil || off != "" {
		t.Fatalf("both off stored as %q, %v", off, err)
	}
}
