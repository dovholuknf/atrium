//go:build integration

package hubstore

import (
	"errors"
	"testing"
)

// A RELEASE DELETES THE CLAIM ONLY WHILE THE ROOM STILL OWNS IT, so a claim that moved meanwhile is left alone.
func TestReleasePRClaimOnlyLetsGoOfTheRoomsOwn(t *testing.T) {
	s := open(t)
	key := PRKey("github.com", "o", "r", 7)
	if _, made, err := s.ClaimPR(key, "beta", "paste"); err != nil || !made {
		t.Fatalf("claim = %v, %v", made, err)
	}
	if _, err := s.ReleasePRClaim(key, "alpha"); !errors.Is(err, ErrNoPRClaim) {
		t.Fatalf("another room's release = %v", err)
	}
	if c, err := s.PRClaimOf(key); err != nil || c.Room != "beta" {
		t.Fatalf("claim after another room's release = %+v, %v", c, err)
	}
	c, err := s.ReleasePRClaim(key, "beta")
	if err != nil || c.Room != "beta" || c.Key != key {
		t.Fatalf("release = %+v, %v", c, err)
	}
	if _, err := s.PRClaimOf(key); err == nil {
		t.Fatal("the claim is still there")
	}
	if _, err := s.ReleasePRClaim(key, "beta"); !errors.Is(err, ErrNoPRClaim) {
		t.Fatalf("a second release = %v", err)
	}
	if _, made, err := s.ClaimPR(key, "alpha", "paste"); err != nil || !made {
		t.Fatalf("a claim after the release = %v, %v", made, err)
	}
}
