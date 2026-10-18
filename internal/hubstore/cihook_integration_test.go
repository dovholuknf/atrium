//go:build integration

package hubstore

import (
	"errors"
	"testing"
	"time"
)

func clockAt(s *Store, t time.Time) *time.Time {
	at := t
	s.SetClock(func() time.Time { return at })
	return &at
}

func TestCIHookSecretIsShownOnceAndStoredHashed(t *testing.T) {
	s := open(t)
	secret, err := s.MintCIHookSecret(false)
	if err != nil || secret == "" {
		t.Fatalf("mint: %q %v", secret, err)
	}
	stored, _ := s.HubSetting(SettingCISecret)
	if stored == "" || stored == secret {
		t.Fatalf("stored %q, want the hash and not the secret", stored)
	}
	if ok, _, _ := s.CIHookSecretOK(secret); !ok {
		t.Fatal("the minted secret was refused")
	}
	if ok, why, _ := s.CIHookSecretOK("nope"); ok || why != "wrong secret" {
		t.Fatalf("a wrong secret answered ok=%v why=%q", ok, why)
	}
	if ok, why, _ := s.CIHookSecretOK(""); ok || why != "no secret" {
		t.Fatalf("a missing secret answered ok=%v why=%q", ok, why)
	}
	if _, err := s.MintCIHookSecret(false); !errors.Is(err, ErrCISecretExists) {
		t.Fatalf("a second mint without rotate: %v", err)
	}
}

func TestCIHookWithNoSecretRefusesEverything(t *testing.T) {
	s := open(t)
	if ok, _, _ := s.CIHookSecretOK("anything"); ok {
		t.Fatal("a hub with no secret accepted one")
	}
}

func TestCIHookRotationKeepsTheOldSecretForADay(t *testing.T) {
	s := open(t)
	start := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	at := clockAt(s, start)
	old, _ := s.MintCIHookSecret(false)
	fresh, err := s.MintCIHookSecret(true)
	if err != nil || fresh == old {
		t.Fatalf("rotate: %q %v", fresh, err)
	}
	if ok, _, _ := s.CIHookSecretOK(fresh); !ok {
		t.Fatal("the new secret was refused")
	}
	*at = start.Add(23 * time.Hour)
	if ok, _, _ := s.CIHookSecretOK(old); !ok {
		t.Fatal("the old secret was refused inside its 24 hours")
	}
	*at = start.Add(25 * time.Hour)
	if ok, why, _ := s.CIHookSecretOK(old); ok || why != "wrong secret" {
		t.Fatalf("the old secret after 24 hours: ok=%v why=%q", ok, why)
	}
	if ok, _, _ := s.CIHookSecretOK(fresh); !ok {
		t.Fatal("the new secret was refused after the grace")
	}
}

func TestCIRunDedupsOnRepoRunAndAttempt(t *testing.T) {
	s := open(t)
	r := CIRun{Repo: "o/r", Workflow: "CI", Branch: "main", SHA: "abc", RunID: 7, Attempt: 1, Conclusion: "failure"}
	if _, dup, err := s.CIRunAdd(r); dup || err != nil {
		t.Fatalf("first: dup=%v %v", dup, err)
	}
	if _, dup, _ := s.CIRunAdd(r); !dup {
		t.Fatal("the same run and attempt was recorded twice")
	}
	r.Attempt = 2
	if _, dup, _ := s.CIRunAdd(r); dup {
		t.Fatal("a re-run is a new attempt and was taken for a repeat")
	}
}

func TestCILastConclusionIsPerRepoWorkflowAndBranch(t *testing.T) {
	s := open(t)
	if _, ok, _ := s.CILastConclusion("o/r", "CI", "main"); ok {
		t.Fatal("an empty table had a last conclusion")
	}
	s.CIRunAdd(CIRun{Repo: "o/r", Workflow: "CI", Branch: "main", RunID: 1, Attempt: 1, Conclusion: "success"})
	s.CIRunAdd(CIRun{Repo: "o/r", Workflow: "CI", Branch: "other", RunID: 2, Attempt: 1, Conclusion: "failure"})
	if c, ok, _ := s.CILastConclusion("o/r", "CI", "main"); !ok || c != "success" {
		t.Fatalf("got %q %v", c, ok)
	}
}

func TestCIDeliveryLifecycleAndSweep(t *testing.T) {
	s := open(t)
	start := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	at := clockAt(s, start)
	run, _, _ := s.CIRunAdd(CIRun{Repo: "o/r", Workflow: "CI", Branch: "main", RunID: 1, Attempt: 1, Conclusion: "failure"})
	id, err := s.CIDeliveryAdd(CIDelivery{RunRef: run.ID, Target: "qa", Kind: "needs", Wake: true, Text: "t"})
	if err != nil {
		t.Fatal(err)
	}
	pend, _ := s.CIDeliveriesPending()
	if len(pend) != 1 || pend[0].ID != id || !pend[0].Wake || pend[0].Repo != "o/r" || pend[0].Branch != "main" {
		t.Fatalf("pending: %+v", pend)
	}
	if err := s.CIDeliverySet(id, CIDelivered, "queued", "r~1"); err != nil {
		t.Fatal(err)
	}
	if pend, _ = s.CIDeliveriesPending(); len(pend) != 0 {
		t.Fatalf("a delivered row is still pending: %+v", pend)
	}
	of, _ := s.CIDeliveriesOf(run.ID)
	if len(of) != 1 || of[0].State != CIDelivered || of[0].Card != "r~1" || of[0].Attempts != 1 {
		t.Fatalf("of: %+v", of)
	}
	*at = start.Add(31 * 24 * time.Hour)
	n, err := s.CISweep()
	if err != nil || n != 1 {
		t.Fatalf("sweep: %d %v", n, err)
	}
	if of, _ = s.CIDeliveriesOf(run.ID); len(of) != 0 {
		t.Fatalf("a swept run kept deliveries: %+v", of)
	}
}
