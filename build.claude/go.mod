// A module of its own, so `go vet ./...`, `go test ./...` and `go build ./...`
// from the repository root stop here. The go tool does not cross into a nested
// module, and build.claude/ is where builds land and scratch programs get
// written, none of which is atrium. Tracked on purpose, and kept by `make clean`.
module atrium-build-output

go 1.26.2
