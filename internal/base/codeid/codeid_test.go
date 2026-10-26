package codeid

import (
	"testing"
	"testing/fstest"
)

func TestOnlyRoomCodeMovesTheFingerprint(t *testing.T) {
	base := fstest.MapFS{
		"go.mod":                    {Data: []byte("module x\n")},
		"internal/a/a.go":           {Data: []byte("package a\n")},
		"internal/api/web/board.js": {Data: []byte("1")},
	}
	want, err := Compute(base)
	if err != nil {
		t.Fatal(err)
	}
	same := fstest.MapFS{
		"go.mod":                    base["go.mod"],
		"internal/a/a.go":           {Data: []byte("package a\r\n")},
		"internal/a/a_test.go":      {Data: []byte("package a")},
		"internal/api/web/board.js": {Data: []byte("2")},
		"docs/x.go":                 {Data: []byte("nope")},
	}
	if got, _ := Compute(same); got != want {
		t.Fatalf("board, docs, tests and CRLF moved it: %s != %s", got, want)
	}
	base["internal/a/a.go"] = &fstest.MapFile{Data: []byte("package b\n")}
	if got, _ := Compute(base); got == want {
		t.Fatal("a Go source change did not move it")
	}
}
