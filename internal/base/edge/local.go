package edge

import (
	"net"
	"net/http"
)

// LocalOperator reports whether a request is the machine's own user, at a
// browser or a shell on this machine, as opposed to anybody a local proxy let
// in. See docs/rnd/local-proxy-trust-design.md.
//
// A LOOPBACK ADDRESS ALONE IS NOT ENOUGH. A share started by hand, `zrok share`
// pointed at 127.0.0.1, connects from loopback on behalf of whoever reached the
// share, so every "loopback means trusted" gate took the internet behind the
// share's basic auth for the operator. Three things, all of them:
//
//  1. the far end is a loopback address,
//  2. the Host names loopback, which a proxy that keeps the public Host fails,
//  3. no forwarding header is present. A header can only refuse: a caller can
//     add one, but cannot strip one its proxy added.
//
// zrok's proxy backend is httputil's reverse proxy: it appends X-Forwarded-For,
// sets X-Proxy, and keeps the public Host, so it fails twice. Still open: a raw
// TCP tunnel (ssh -L, socat), which carries no header and keeps a loopback Host,
// and needs a machine account or a private token to set up. Security stage 2.
func LocalOperator(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return false
	}
	return LoopbackHost(r.Host) && !ViaProxy(r)
}

// proxyHeaders are the headers a forwarding proxy adds.
var proxyHeaders = []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-Ip", "X-Proxy"}

// ViaProxy reports whether a request carries a header a forwarding proxy adds.
func ViaProxy(r *http.Request) bool {
	for _, h := range proxyHeaders {
		if r.Header.Get(h) != "" {
			return true
		}
	}
	return false
}

// ProxyNote is the line a refusal adds when a proxy is why: "" otherwise.
func ProxyNote(r *http.Request) string {
	if !ViaProxy(r) && !(LoopbackRemote(r.RemoteAddr) && !LoopbackHost(r.Host)) {
		return ""
	}
	return ". this request came through a proxy (zrok or similar). run it from a browser or shell on the machine itself"
}

// LoopbackRemote reports whether a RemoteAddr is a loopback address. On its own
// it says nothing about who is asking. See LocalOperator.
func LoopbackRemote(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
