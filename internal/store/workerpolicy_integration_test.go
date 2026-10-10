//go:build integration

package store

import "testing"

func TestWorkerPolicyIsOffUntilSet(t *testing.T) {
	s := openTestStore(t)
	if p := s.WorkerPolicy(); p != (WorkerPolicy{}) {
		t.Fatalf("policy %+v on a fresh store", p)
	}
}

func TestWorkerPolicyRoundTripsAndOffIsEmpty(t *testing.T) {
	s := openTestStore(t)
	v, err := CheckWorkerPolicy(WorkerPolicy{Model: " sonnet ", BudgetUSD: 12.345})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(SettingWorkerPolicy, v); err != nil {
		t.Fatal(err)
	}
	if p := s.WorkerPolicy(); p.Model != "sonnet" || p.BudgetUSD != 12.35 {
		t.Fatalf("read back %+v", p)
	}
	if off, err := CheckWorkerPolicy(WorkerPolicy{}); err != nil || off != "" {
		t.Fatalf("both off stored as %q, %v", off, err)
	}
}

func TestWorkerPolicyRefusesNonsense(t *testing.T) {
	for _, p := range []WorkerPolicy{{BudgetUSD: -1}, {BudgetUSD: MaxWorkerBudgetUSD + 1}, {Model: "two words"},
		{Model: "a;b"}} {
		if _, err := CheckWorkerPolicy(p); err == nil {
			t.Fatalf("%+v was taken", p)
		}
	}
}
