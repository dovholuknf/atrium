# r-new-sec-go-toolchain: built on Go 1.26.2, with 11 standard library vulns atrium calls

Status: held (pause, 2026-10-01). @runtime. High. Source: docs/backlog/review/review-new-security-audit-kimi.md (the Kimi audit, checked by @review on 2026-10-01).

## Why

go.mod:3 says `go 1.26.2`, and CI and releases build with it (`go-version-file: go.mod`). govulncheck v1.8.0 (DB
2026-10-01) reports 11 called stdlib vulns, fixed in 1.26.3 to 1.26.6: crypto/tls GO-2026-6090 and 5856, net/http
6089, 4918 and 5026, net/http/httputil 4976 (the hub proxy, internal/link/proxy.go:695), encoding/asn1 5972,
encoding/xml 6088, net/url 6218, crypto/x509 5037, net/textproto 5039 and net 4971. One module is also hit:
go-jose/v4 v4.1.3 GO-2026-4945, a JWE panic reported as called through the ziti and zrok SDKs, fixed in 4.1.4.
golang.org/x/net v0.55.0 closes the x/net side of 5026.

## Wanted

- `go 1.26.6` (or newer) in go.mod, go-jose v4.1.4 and x/net v0.55.0. Run the package tests and `govulncheck
  ./...` clean of called vulns.
- `govulncheck ./...` as a CI step, so the next one is not found by an audit.
