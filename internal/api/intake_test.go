package api

import "testing"

// A source is a shell script, and a shell script should not have to wrap one
// item in brackets to say one thing. `gh issue list --json` produces an array.
// Both arrive here and both have to work without a flag saying which.

func TestIntakeTakesOneItemOrMany(t *testing.T) {
	one := `{"source":"github","external_id":"4211","title":"x"}`
	items, err := decodeIntake([]byte(one))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ExternalID != "4211" {
		t.Fatalf("one item decoded as %+v", items)
	}

	many := `[{"source":"github","external_id":"1"},{"source":"github","external_id":"2"}]`
	items, err = decodeIntake([]byte(many))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("an array decoded as %d items", len(items))
	}
}

// Leading whitespace decides nothing. A script that pretty-prints its output
// still gets read as an array.
func TestIntakeIgnoresLeadingWhitespace(t *testing.T) {
	items, err := decodeIntake([]byte("\n\t  [{\"source\":\"ci\",\"external_id\":\"9\"}]"))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Source != "ci" {
		t.Fatalf("decoded as %+v", items)
	}
}

// An empty body is a source that produced nothing and said so badly. Refused,
// rather than read as zero items, because zero items and a broken script look
// identical afterwards and only one of them is fine.
func TestIntakeRefusesAnEmptyBody(t *testing.T) {
	for _, raw := range []string{"", "   ", "\n"} {
		if _, err := decodeIntake([]byte(raw)); err == nil {
			t.Fatalf("an empty body (%q) was accepted", raw)
		}
	}
}

func TestIntakeRefusesGarbage(t *testing.T) {
	if _, err := decodeIntake([]byte("not json at all")); err == nil {
		t.Fatal("a non-JSON body was accepted")
	}
	if _, err := decodeIntake([]byte(`[{"source":`)); err == nil {
		t.Fatal("a truncated array was accepted")
	}
}
