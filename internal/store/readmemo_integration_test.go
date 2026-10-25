//go:build integration

package store

import "testing"

func TestMemoizedReadsAreDroppedByAWrite(t *testing.T) {
	s := openTestStore(t)
	if err := s.SetSetting("memo_probe", "one"); err != nil {
		t.Fatal(err)
	}
	s.Memoized(func() {
		for i := 0; i < 2; i++ {
			if v, _ := s.Setting("memo_probe"); v != "one" {
				t.Fatalf("read %q, wanted one", v)
			}
		}
		if err := s.SetSetting("memo_probe", "two"); err != nil {
			t.Fatal(err)
		}
		if v, _ := s.Setting("memo_probe"); v != "two" {
			t.Fatalf("read %q after a write, wanted two", v)
		}
	})
	if _, err := s.db.Exec(`UPDATE setting SET value = 'three' WHERE key = 'memo_probe'`); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Setting("memo_probe"); v != "three" {
		t.Fatalf("read %q outside a scope, wanted three", v)
	}
}
