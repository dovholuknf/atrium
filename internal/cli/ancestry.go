package cli

// Shared by every ancestry_*.go, so the list of runners and the bound on the
// walk cannot drift between platforms.

// runnerNames are the process names a claude session runs under. Checked
// without an extension, since that differs by platform and by how it was
// installed.
var runnerNames = map[string]bool{"claude": true, "node": true}

// maxHops bounds the walk. A hook is two or three processes below its runner,
// and a bound means a corrupt or cyclic parent chain cannot spin here.
const maxHops = 6
