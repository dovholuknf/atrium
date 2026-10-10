//go:build integration

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func oldAgentName(override string) string {
	if override != "" {
		return override
	}
	if v := os.Getenv("ATRIUM_AGENT_NAME"); v != "" {
		return v
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	return filepath.Base(cwd)
}

func oldHookInline(name, cwd string) string {
	agent := name
	if agent == "" {
		agent = os.Getenv("ATRIUM_AGENT_NAME")
	}
	if agent == "" && cwd != "" {
		agent = filepath.Base(cwd)
	}
	return agent
}

func TestNameDerivationKeepsEachCallersOutput(t *testing.T) {
	cwd, _ := os.Getwd()
	for _, env := range []string{"", "from-env"} {
		t.Setenv("ATRIUM_AGENT_NAME", env)
		for _, name := range []string{"", "told"} {
			for _, dir := range []string{"", filepath.Join("some", "checkout")} {
				// agentName (join, /permission)
				if got, want := agentName(name), oldAgentName(name); got != want {
					t.Errorf("agentName(%q) env=%q: %q, was %q", name, env, got, want)
				}
				// the activity hook's inline derivation
				got, src := hookAgent(name, dir)
				if want := oldHookInline(name, dir); got != want {
					t.Errorf("hook(%q,%q) env=%q: %q, was %q", name, dir, env, got, want)
				}
				// hookAgent's own source, as the session and turn hooks send it
				wantSrc := ""
				if name == "" && env == "" && dir != "" {
					wantSrc = "dir"
				}
				if src != wantSrc {
					t.Errorf("hook(%q,%q) env=%q: source %q, was %q", name, dir, env, src, wantSrc)
				}
			}
		}
	}
	t.Setenv("ATRIUM_AGENT_NAME", "")
	if a, s := agentNameSource(""); a != filepath.Base(cwd) || s != "dir" {
		t.Errorf("agentNameSource from the directory: %q %q", a, s)
	}
	if a, s := agentNameSource("told"); a != "told" || s != "" {
		t.Errorf("agentNameSource told: %q %q", a, s)
	}
}
