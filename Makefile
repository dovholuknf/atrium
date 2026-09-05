# Atrium build commands. Output lands in build.claude/ per the project convention.

OUT      := build.claude
BINARY   := $(OUT)/atrium.exe
PKG      := ./cmd/atrium

# What this build calls itself.
#
# A tag when the tree is on one, otherwise `dev`. NOT the nearest tag plus a
# count: a binary built from a working tree is not a release and should not
# claim a number close to one, because the number is what a package manager
# compares to decide whether you already have it.
#
# `git describe --exact-match` answers nothing and fails when there is no tag on
# HEAD, which is the ordinary case, so the failure is the answer.
VERSION  := $(shell git describe --tags --exact-match 2>/dev/null || echo dev)
COMMIT   := $(shell git rev-parse HEAD 2>/dev/null)
LDFLAGS  := -X github.com/dovholuknf/atrium/internal/cli.Version=$(VERSION) \
            -X github.com/dovholuknf/atrium/internal/cli.Commit=$(COMMIT)

.PHONY: build release run-status run-watch run-serve tidy test check clean

# Everything that is checked without running anything.
#
# `go test` covers the Go. The other two cover the parts a compiler never sees:
# the board is one HTML file with a large script block in it, and the PowerShell
# scripts register scheduled tasks, so neither can be verified by trying it.
check: test
	bash scripts/check-board.sh
	bash scripts/check-skins.sh
	pwsh -NoProfile -File scripts/check-powershell.ps1 || \
		powershell.exe -NoProfile -File scripts/check-powershell.ps1

build:
	@mkdir -p $(OUT)
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

# Every platform, named and hashed, ready for a package manifest to point at.
#
# Separate from `build` because it is slow and because it is the only target
# whose output leaves this machine. See `docs/packaging.md`.
release:
	bash scripts/release.sh $(VERSION)

run-status: build
	$(BINARY) status

run-watch: build
	$(BINARY) watch

run-serve: build
	$(BINARY) serve

tidy:
	go mod tidy

test:
	go test ./...

clean:
	rm -rf $(OUT)
