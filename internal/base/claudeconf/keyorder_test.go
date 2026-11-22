package claudeconf

import (
	"encoding/json"
	"testing"
)

func TestMarshalInOrderKeepsTheFilesKeyOrder(t *testing.T) {
	orig := []byte(`{"zeta": 1, "model": "opus", "alpha": {"b": 2, "a": [1, 2]}}`)
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(orig, &doc); err != nil {
		t.Fatal(err)
	}
	doc["hooks"] = json.RawMessage(`{"Stop": []}`)
	doc["beta"] = json.RawMessage(`true`)
	got, err := marshalInOrder(orig, doc)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n" +
		"  \"zeta\": 1,\n" +
		"  \"model\": \"opus\",\n" +
		"  \"alpha\": {\n    \"b\": 2,\n    \"a\": [\n      1,\n      2\n    ]\n  },\n" +
		"  \"beta\": true,\n" +
		"  \"hooks\": {\n    \"Stop\": []\n  }\n" +
		"}"
	if string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestMarshalInOrderMatchesMarshalIndentForANewFile(t *testing.T) {
	doc := map[string]json.RawMessage{"b": json.RawMessage(`{"x":1}`), "a": json.RawMessage(`"<&>"`)}
	got, err := marshalInOrder(nil, doc)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.MarshalIndent(doc, "", "  ")
	if string(got) != string(want) {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if got, _ := marshalInOrder(nil, map[string]json.RawMessage{}); string(got) != "{}" {
		t.Fatalf("empty: %s", got)
	}
}
