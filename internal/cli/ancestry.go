package cli

// Shared by every ancestry_*.go, so the list of runners and the bound on the
// walk cannot drift between platforms.

// runnerNames are the process names a runner session runs under. Checked
// without an extension, since that differs by platform and by how it was
// installed.
//
// opencode runs its hooks as a plugin inside its own process, so the hook's
// parent IS the runner. Without it here the walk goes past opencode and claims
// whatever claude or node started the room.
var runnerNames = map[string]bool{"claude": true, "node": true, "opencode": true}

// maxHops bounds the walk. A hook is two or three processes below its runner,
// and a bound means a corrupt or cyclic parent chain cannot spin here.
const maxHops = 6
