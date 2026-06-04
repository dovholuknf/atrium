# Atrium build commands. Output lands in build.claude/ per the project convention.

OUT      := build.claude
BINARY   := $(OUT)/atrium.exe
PKG      := ./cmd/atrium

.PHONY: build run-status run-watch run-serve tidy test clean

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
