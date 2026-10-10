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
# Tracked files only. Go's own dirty flag counts untracked ones, and this
# checkout always has some. See `Tree` in internal/cli/version.go.
TREE     := $(if $(shell git status --porcelain --untracked-files=no 2>/dev/null),modified,clean)
LDFLAGS  := -X github.com/dovholuknf/atrium/internal/cli.Version=$(VERSION) \
            -X github.com/dovholuknf/atrium/internal/cli.Commit=$(COMMIT) \
            -X github.com/dovholuknf/atrium/internal/cli.Tree=$(TREE)

.PHONY: build release tidy test test-all check clean

# Everything that is checked without running anything.
#
# `go test` covers the Go. The other two cover the parts a compiler never sees:
# the board is one HTML file with a large script block in it, and the PowerShell
# scripts register scheduled tasks, so neither can be verified by trying it.
check: test
	bash scripts/check-board.sh
	bash scripts/check-skins.sh
	node scripts/check-contrast.js internal/api/web/css
	pwsh -NoProfile -File scripts/check-powershell.ps1 || \
		powershell.exe -NoProfile -File scripts/check-powershell.ps1

build:
	@mkdir -p $(OUT)
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

# Every platform, named and hashed, ready for a package manifest to point at.
#
# Separate from `build` because it is slow and because it is the only target
# whose output leaves this machine. See `docs/release/packaging.md`.
release:
	bash scripts/release.sh $(VERSION)

tidy:
	go mod tidy

# Unit tests. `test-all` adds the integration tests, which CI runs. See scripts/test.sh.
test:
	bash scripts/test.sh unit

test-all:
	bash scripts/test.sh all

# Keeps $(OUT)/go.mod, which fences build output off from `go vet ./...`.
clean:
	find $(OUT) -mindepth 1 ! -name go.mod -delete 2>/dev/null || true
