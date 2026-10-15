package api

import "testing"

func TestNormCwd(t *testing.T) {
	for in, want := range map[string]string{
		`C:\wt\a\`:    "C:/wt/a",
		"C:/wt//a/":   "C:/wt/a",
		"/home/x/wt/": "/home/x/wt",
		"/":           "/",
		"  /a/b  ":    "/a/b",
		"":            "",
		`\\?\C:\wt\a`: "C:/wt/a",
	} {
		if got := normCwd(in); got != want {
			t.Errorf("normCwd(%q) = %q, want %q", in, got, want)
		}
	}
	if !samePathFold("/a/b/", "/a/b") || samePathFold("/a/b", "/a/c") || samePathFold("", "") {
		t.Error("samePathFold")
	}
}
