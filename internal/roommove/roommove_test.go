package roommove

import (
	"io"
	"testing"
)

func TestRunRefusesBeforeItAsksAnyRoom(t *testing.T) {
	for _, o := range []Opts{{From: "a"}, {To: "a"}, {From: "a", To: "a"}} {
		if err := Run(io.Discard, "c1", o); err == nil {
			t.Errorf("%+v moved", o)
		}
	}
}

func TestARecordComesBackAtTheStepItWasSavedAt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	r := &Record{ID: NewID(), Card: "c1", From: "a", To: "b"}
	if err := r.save(stepParked); err != nil {
		t.Fatal(err)
	}
	got, err := Load(r.ID)
	if err != nil || got.Step != stepParked || got.Card != "c1" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := Load("mv-none"); err == nil {
		t.Fatal("loaded a move that was never saved")
	}
}
