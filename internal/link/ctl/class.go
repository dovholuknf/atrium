package ctl

import "github.com/modelcontextprotocol/go-sdk/mcp"

// WorkerTools is the whole of the worker set, by name.
//
// A TOOL IS IN THE WORKER SET ONLY WHEN IT IS NAMED HERE. `AddTool` filters on
// this list and not on a list of exclusions, so a tool added later, such as the
// git tools a `registerGit` adds, lands in `full` alone with no further change.
var WorkerTools = map[string]bool{
	"atrium_status": true,
	"atrium_peers":  true,
	"atrium_say":    true,
	"atrium_report": true,
	// How a worker ends, in one line. See ctl_end.go.
	"atrium_done":    true,
	"atrium_blocked": true,
	// How a worker says what a test found, pinned to its own commit. See record_mcp.go.
	"atrium_record": true,
	"atrium_task":   true,
	"atrium_alias":  true,
	// A lean worker names what it lacks. See needHandler.
	"atrium_need": true,
	// Workers write the reports, so a worker publishes. See docs_mcp.go.
	"atrium_publish": true,
	// Workers hand files between rooms. See files_mcp.go.
	"atrium_put":    true,
	"atrium_get":    true,
	"atrium_files":  true,
	"atrium_export": true,
	// The cards that push to the hub are workers. See git_hub.go.
	"atrium_git_push": true,
	// And the ones that read code that is not in their cwd. See git_url.go.
	"atrium_git_url": true,
	// And the ones that read the CI of a repository, which the hub reads for them. See ci_door.go.
	"atrium_ci": true,
	// And the ones that need a build machine. See resources_mcp.go.
	"atrium_resources": true,
	// And the ones that clear up cards, which otherwise script the API by hand. See closeHandler.
	"atrium_close": true,
	// And the ones that file what a run cost and what slowed it, for the work ledger. See backlog_mcp.go.
	"atrium_reports": true,
	// And the ones that land their branch, through the merge queue. See merge_mcp.go.
	"atrium_merge": true,
}

// ItemWithheld is what an item worker loses from the worker set: every way to talk to anything but the tool that ends
// its work. The same four the say endpoint refuses for, so a worker that reached one by lying about its class meets
// the refusal there.
var ItemWithheld = map[string]bool{
	"atrium_say": true, "atrium_peers": true, "atrium_task": true, "atrium_report": true,
}

// InClass reports whether a tool is served to a class.
func InClass(name string, class Class) bool {
	if name == "atrium_review" {
		return class == ClassWorkerReview
	}
	if class == ClassWorkerReview {
		return WorkerTools[name]
	}
	if class == ClassWorkerItem {
		return WorkerTools[name] && !ItemWithheld[name]
	}
	return class == ClassFull || class == ClassFullResident || WorkerTools[name]
}

// AddTool is `mcp.AddTool` for the class being built: it registers the tool when
// the class includes it and does nothing otherwise.
func AddTool[In, Out any](s *mcp.Server, class Class, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	if InClass(t.Name, class) {
		mcp.AddTool(s, t, h)
	}
}
