package daemon

import "github.com/dovholuknf/atrium/internal/daemon/roomconf"

// What the roomconf package declares and the CLI reaches as daemon.X.
type Location = roomconf.Location

var DefaultLocationPath = roomconf.DefaultLocationPath
var LocationPath = roomconf.LocationPath
var ReadLocation = roomconf.ReadLocation
var SharedLocationPath = roomconf.SharedLocationPath
