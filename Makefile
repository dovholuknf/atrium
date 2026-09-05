# Atrium build commands. Output lands in build.claude/ per the project convention.

OUT      := build.claude
BINARY   := $(OUT)/atrium.exe
PKG      := ./cmd/atrium

.PHONY: build run-status run-watch run-serve tidy test check clean

# Everything that is checked without running anything.
#
# `go test` covers the Go. The other two cover the parts a compiler never sees:
# the board is one HTML file with a large script block in it, and the PowerShell
# scripts register scheduled tasks, so neither can be verified by trying it.
check: test
	bash scripts/check-board.sh
	pwsh -NoProfile -File scripts/check-powershell.ps1 || \
		powershell.exe -NoProfile -File scripts/check-powershell.ps1

build:
	@mkdir -p $(OUT)
	go build -o $(BINARY) $(PKG)

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
