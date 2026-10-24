package rules

import "testing"

func TestWorkerPolicyRefusesNonsense(t *testing.T) {
	t.Parallel()
	for _, p := range []WorkerPolicy{{BudgetUSD: -1}, {BudgetUSD: MaxWorkerBudgetUSD + 1}, {Model: "two words"},
		{Model: "a;b"}} {
		if _, err := CheckWorkerPolicy(p); err == nil {
			t.Fatalf("%+v was taken", p)
		}
	}
}
