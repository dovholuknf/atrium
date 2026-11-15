package transport

import (
	"strings"
	"testing"
)

type fakeKind struct{}

func (fakeKind) Kind() string { return "fake" }
func (fakeKind) Fields() []AccessField {
	return []AccessField{{Name: "addr", Label: "Address", Required: true}}
}
func (fakeKind) Hub(Entry) (HubAccess, error)   { return nil, nil }
func (fakeKind) Room(Entry) (RoomAccess, error) { return nil, nil }

func TestAccessListAllowsManyPerKindAndChecksTheForm(t *testing.T) {
	RegisterAccess(fakeKind{})
	ok := func(id string) Entry {
		return Entry{ID: id, Name: id, Kind: "fake", Settings: map[string]string{"addr": "x"}}
	}
	if err := ValidateEntries([]Entry{ok("a"), ok("b"), ok("c")}); err != nil {
		t.Fatalf("three of one kind: %v", err)
	}
	if err := ValidateEntries([]Entry{ok("a"), ok("a")}); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("duplicate id: %v", err)
	}
	bad := ok("z")
	bad.Settings = nil
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("missing field: %v", err)
	}
	unk := ok("u")
	unk.Kind = "nope"
	if err := unk.Validate(); err == nil {
		t.Fatal("unknown kind accepted")
	}
}
