package forge

import (
	"github.com/dovholuknf/atrium/internal/link/conn"
	"github.com/dovholuknf/atrium/internal/link/ctl"
	"github.com/dovholuknf/atrium/internal/link/jsonio"
)

var (
	crJSON      = jsonio.JSON
	crFail      = jsonio.Fail
	crDecode    = jsonio.Decode
	crDecodeMax = jsonio.DecodeMax
	ffail       = jsonio.StoreFail
)

const (
	ForgeRoomHeader   = conn.ForgeRoomHeader
	PRClaimPrefix     = conn.PRClaimPrefix
	PRClaimRoomHeader = conn.PRClaimRoomHeader
)

type (
	PRClaimAnswer = conn.PRClaimAnswer
	LaunchInput   = ctl.LaunchInput
)

const RelayCard = conn.RelayCard

func relaySuperlean(on bool) *bool {
	if !on {
		return nil
	}
	return &on
}

var writeJSONBody = jsonio.WriteJSONBody
