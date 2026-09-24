// Not Go. This file makes website/ a module of its own, so `go vet ./...` and `go build ./...` from the repository
// root never walk into website/node_modules, where an npm package is free to ship .go files.
module github.com/dovholuknf/atrium/website

go 1.22
