CI now runs `scripts/check-imports.go`, which pins the Go import graph to a layer table and fails on a new edge or an unlisted package. r-go-modules-split.
