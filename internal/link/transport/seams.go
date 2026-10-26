package transport

import (
	"bufio"
	"net"
)

// Names the link package's tests reach for, because they drive the transport through its wire formats.
type (
	Token     = token
	EnrolReq  = enrolReq
	EnrolResp = enrolResp
)

const TokenPrefix = tokenPrefix

var (
	HandshakeClient  = handshakeClient
	IsLoopbackDirect = isLoopbackDirect
)

// NewLegacyConn is a connection that arrived on an overlay listener without TLS.
func NewLegacyConn(c net.Conn, overlay string) net.Conn {
	return &legacyConn{peeked: &peeked{Conn: c, r: bufio.NewReader(c)}, transport: overlay}
}
