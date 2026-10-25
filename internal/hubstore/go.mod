module github.com/dovholuknf/atrium/internal/hubstore

go 1.26.10

require (
	github.com/dovholuknf/atrium/internal/base v0.0.0
	github.com/dovholuknf/atrium/internal/gitsync v0.0.0
	github.com/google/uuid v1.6.0
	go.yaml.in/yaml/v3 v3.0.4
	modernc.org/sqlite v1.57.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	modernc.org/libc v1.74.4 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

replace (
	github.com/dovholuknf/atrium/internal/base => ../base
	github.com/dovholuknf/atrium/internal/gitsync => ../gitsync
)
