package store

import (
	"strings"
	"testing"
)

func TestParseLocalServersTakesAListAndRefusesWhatCannotRun(t *testing.T) {
	got, err := ParseLocalServers(`[{"name":"llama-server","cmd":"llama-server","args":["--port","8080"],` +
		`"health":"http://127.0.0.1:8080/health"}]`)
	if err != nil || len(got) != 1 || got[0].Name != "llama-server" || got[0].Args[1] != "8080" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got, err := ParseLocalServers("  "); err != nil || got != nil {
		t.Fatalf("empty: got %+v, %v", got, err)
	}
	for raw, want := range map[string]string{
		`{"name":"x"}`: "not a JSON list",
		`[{"name":"../x","cmd":"a","health":"http://h"}]`:                                         "is not letters",
		`[{"name":"x","cmd":" ","health":"http://h"}]`:                                            "has no cmd",
		`[{"name":"x","cmd":"a","health":"127.0.0.1:8080"}]`:                                      "needs a health URL",
		`[{"name":"x","cmd":"a","health":"http://h"},{"name":"x","cmd":"b","health":"http://h"}]`: "named twice",
	} {
		if _, err := ParseLocalServers(raw); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err %v, want %q", raw, err, want)
		}
	}
}
