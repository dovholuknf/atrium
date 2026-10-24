module github.com/dovholuknf/atrium/internal/ptyhost

go 1.26.10

require (
	github.com/Microsoft/go-winio v0.6.2
	github.com/aymanbagabas/go-pty v0.2.3
	github.com/dovholuknf/atrium/internal/base v0.0.0
	golang.org/x/sys v0.48.0
)

require (
	github.com/creack/pty v1.1.24 // indirect
	github.com/u-root/u-root v0.16.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
)

replace github.com/dovholuknf/atrium/internal/base => ../base
