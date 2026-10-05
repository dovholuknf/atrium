# r-new-sec-go-toolchain

## Changed
- go.mod: `go 1.26.2` to `go 1.26.6`, golang.org/x/net v0.53.0 to v0.55.0, go-jose/v4 v4.1.3 to v4.1.4. go.sum from `go mod tidy`. Nothing else moved.
- scripts/ci.sh: a govulncheck step (`go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...`). The workflow calls ci.sh, so it needed no edit. It needs network in CI.
- changelog/runtime/2026-10-04-r-new-sec-go-toolchain.md.

## govulncheck (v1.8.0)
- Before (on go1.26.4 locally): 8 affected vulnerabilities, the stdlib ones in net/url, crypto/tls, net/http, encoding/xml and encoding/asn1, plus x/net GO-2026-5026 and go-jose GO-2026-4945.
- After (go1.26.6): `No vulnerabilities found`, 0 called. It still lists 14 in imported packages and 7 in required modules that the code does not call.

## Tests
- `go test ./...` with ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG cleared: only the known failures (TestGlobalAutoSurvivesAReopen, TestAnOlderClaudeIsStartedWithoutTheFlag..., TestCloneMakesTheCloneAtHostOwnerRepo, TestPushToHubRefuses...). internal/link failed once in the first run and passed in the second, so it looks flaky and not related.
- Cross builds for darwin/arm64 and linux/amd64 succeed.
- scripts/ci.sh itself was not run end to end.

## Every room's build
Go 1.26.6 or newer is required. A local Go 1.21 or newer fetches it on its own through GOTOOLCHAIN=auto (the default), as it did here (it fetched 1.26.8 and 1.26.6). A room with GOTOOLCHAIN=local needs 1.26.6 installed. CI's setup-go reads go.mod so it follows.
