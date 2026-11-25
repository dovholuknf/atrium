package runnersetup

import (
	"path/filepath"
	"strings"
)

// Codex. Its trust, sandbox and hooks are written at launch by the daemon, so the one check here is the code mode
// host: codex 0.155 runs every tool call on an OpenAI model through codex-code-mode-host beside its own binary, fails
// closed without it, and a standalone install does not always ship it.
var Codex = &Adapter{
	ID: "codex", Label: "codex", Cmds: []string{"codex"}, Package: "@openai/codex",
	Checks: []Check{
		{ID: "code-mode-host", Label: "code mode host", Run: codexCodeModeHostCheck},
	},
}

func codexCodeModeHostCheck(env Env) Result {
	exe := env.Exe
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	ext := ""
	if env.goos() == "windows" {
		ext = ".exe"
	}
	if !strings.EqualFold(filepath.Base(exe), "codex"+ext) {
		return Result{State: NA, Detail: "atrium checks only a standalone codex binary for its code mode host."}
	}
	host := filepath.Join(filepath.Dir(exe), "codex-code-mode-host"+ext)
	r := Result{Path: filepath.ToSlash(host)}
	if exists(host) {
		r.State, r.Detail = OK, "beside codex."
		return r
	}
	r.State, r.Fix, r.Command = Fail, FixExplain, "codex --version"
	r.Detail = "codex runs every tool call on an OpenAI model through its code mode host, and fails closed " +
		"without it, so a session cannot reach atrium-control. download codex-code-mode-host for this " +
		"platform from the codex release that matches codex --version, and save it as " + r.Path + "."
	return r
}
