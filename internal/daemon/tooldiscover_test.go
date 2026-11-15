package daemon

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFindToolOrAltAcceptsAnyCompilerName(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "clang")
	if findToolOrAlt("cc", dir) == "" || findToolOrAlt("gcc", dir) == "" || findToolOrAlt("c++", dir) != "" {
		t.Fatal("alternates not honoured")
	}
}

func TestToolsLineNamesTheCompilerAndItsEnv(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "gcc")
	fakeTool(t, dir, "g++")
	got := toolsLine(dir)
	cc := filepath.ToSlash(filepath.Join(dir, "gcc"))
	for _, want := range []string{"cc=" + cc, "c++=", "CC=" + cc, "CXX=", "CGO_ENABLED=1", "yours to install", "never report it as a blocker"} {
		if !strings.Contains(got, want) {
			t.Fatalf("toolsLine lacks %q: %s", want, got)
		}
	}
}

func TestWithToolDirsHandsOverCCButNotOverAnOperatorsOwn(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "gcc")
	got := strings.Join(withToolDirs([]string{"PATH=x"}, dir), "\n")
	if !strings.Contains(got, "CC=") || !strings.Contains(got, "CGO_ENABLED=1") {
		t.Fatalf("env = %s", got)
	}
	got = strings.Join(withToolDirs([]string{"PATH=x", "CC=mine"}, dir), "\n")
	if strings.Count(got, "CC=") != 1 {
		t.Fatalf("env = %s", got)
	}
}
