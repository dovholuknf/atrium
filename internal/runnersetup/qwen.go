package runnersetup

import "github.com/dovholuknf/atrium/internal/base/claudeconf"

// Qwen Code, measured against 0.27.3's hooks doc and bundle. Hooks are claude's, in ~/.qwen/settings.json, with a
// PermissionRequest hook atrium answers. A project's own hooks need an approval a person gives, user ones do not.
var Qwen = &Adapter{
	ID: "qwen", Label: "qwen code", Cmds: []string{"qwen"}, Package: "@qwen-code/qwen-code",
	Checks: []Check{
		{ID: "hooks", Label: "atrium hooks", Run: hooksCheck(claudeconf.Qwen), Apply: hooksApply(claudeconf.Qwen)},
	},
	Launch: hooksLaunch(claudeconf.Qwen),
}
