package daemon

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestToolchainMissingLooksOnTheRunnersPath(t *testing.T) {
	t.Parallel()
	has := map[string]bool{
		filepath.Join("repo", "go.mod"):   true,
		filepath.Join("repo", "Makefile"): true,
		filepath.Join("tools", "go.exe"):  true,
		filepath.Join("bin", "make"):      true,
	}
	exists := func(p string) bool { return has[p] }
	cases := []struct {
		name, goos, cwd string
		env             []string
		want            []string
		checked         bool
	}{
		{"go off the path", "linux", "repo", []string{"PATH=bin:tools2"}, []string{"go"}, true},
		{"windows pathext", "windows", "repo", []string{"Path=tools;x", "PATHEXT=.COM;.EXE"}, []string{"make"}, true},
		{"none on path", "windows", "repo", []string{"PATH="}, []string{"go", "make"}, true},
		{"not a go repo", "linux", "elsewhere", []string{"PATH="}, nil, false},
	}
	for _, c := range cases {
		got, checked := toolchainMissing(c.goos, c.cwd, c.env, exists)
		if !reflect.DeepEqual(got, c.want) || checked != c.checked {
			t.Errorf("%s: got %v %v, wanted %v %v", c.name, got, checked, c.want, c.checked)
		}
	}
}

func TestHeardNotFoundSaysSoOncePerTool(t *testing.T) {
	t.Parallel()
	var s toolchainState
	s.missing = []string{"make"}
	if !s.heardNotFound("c1", "go") {
		t.Fatal("first go not heard")
	}
	if s.heardNotFound("c2", "go") {
		t.Fatal("second go heard again")
	}
	if !s.heardNotFound("c2", "make") {
		t.Fatal("first make not heard")
	}
	if !reflect.DeepEqual(s.missing, []string{"make", "go"}) || s.card != "c2" {
		t.Fatalf("got %v from %s", s.missing, s.card)
	}
}
