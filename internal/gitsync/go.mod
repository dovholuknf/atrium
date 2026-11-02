module github.com/dovholuknf/atrium/internal/gitsync

go 1.26.10

require (
	github.com/dovholuknf/atrium/internal/base v0.0.0
	golang.org/x/net v0.61.0
	golang.org/x/sys v0.49.0
)

require golang.org/x/text v0.43.0 // indirect

replace github.com/dovholuknf/atrium/internal/base => ../base
