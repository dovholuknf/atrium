# REPORT

Changed: hook_permission.go now denies ("the edit did not parse, nothing was run") when an approval carries an edit that does not parse back into the raw-JSON tool input. Before, it sent a plain allow and the original call ran.

Tests: new subtests in TestPermDecisions cover an MCP call and apply_patch (deny) and a parsing edit (still allows). TestPerm* passes.

Also: gofmt of internal/daemon/fyi_test.go in its own commit. Changelog added. Nothing left.
