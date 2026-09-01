package api

import (
	"embed"
	"io/fs"
	"net/http"
)

// web holds the built client. The agreed client is a React SPA, but this is a
// plain page for now: it exercises the same JSON plus SSE contract, so it
// proves the API without a node toolchain standing between us and a running
// prototype. Replacing it changes nothing on the server.
//
//go:embed web
var web embed.FS

func webHandler() http.Handler {
	sub, err := fs.Sub(web, "web")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
