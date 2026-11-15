//go:build integration

package hubstore

import "testing"

func TestAPendingRoomWaitsUntilApproved(t *testing.T) {
	s := open(t)
	r, err := s.AddPending("lab", TransportZiti)
	if err != nil || r.State != StatePending {
		t.Fatalf("pending: %+v %v", r, err)
	}
	again, err := s.AddPending("lab", TransportZiti)
	if err != nil || again.ID != r.ID {
		t.Fatalf("a redial made a second row: %+v %v", again, err)
	}
	if err := s.Approve("lab"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ByName("lab")
	if got.State != StateActive {
		t.Fatalf("after approve: %s", got.State)
	}
	if _, err := s.AddPending("lab", TransportZiti); err == nil {
		t.Fatal("an approved room's name was taken as pending")
	}
}
