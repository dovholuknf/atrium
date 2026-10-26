package link

import "github.com/dovholuknf/atrium/internal/link/transport"

type (
	token     = transport.Token
	enrolReq  = transport.EnrolReq
	enrolResp = transport.EnrolResp
)

const tokenPrefix = transport.TokenPrefix

var (
	handshakeClient = transport.HandshakeClient
	legacyTransport = transport.LegacyTransport
)
