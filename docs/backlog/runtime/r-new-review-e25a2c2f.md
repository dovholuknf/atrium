# Review of e25a2c2f (@orchestrator: `ATRIUM_HOSTS` takes `*.domain`)

Reviewed by @review, 2026-09-30, from `git diff a06ca6ce...e25a2c2f` (`internal/edge/edge.go`, `edge_test.go`).
Hub side. Read closely, since it changes the rebinding guard. `go vet` and `go test ./internal/edge/` pass, plus a
scratch test of the cases below (removed after).

## What holds

- **The match is anchored on a label.** `hostSet.has` compares against `"."+suffix`, so `evilshares.zrok.io` and
  `shares.zrok.io.evil.example` are refused, and the domain alone and `.shares.zrok.io` are refused by the length check.
- **Case and forms.** `hostOf` lowercases both sides, so `A.SHARES.ZROK.IO` matches. `*.x:443` and
  `https://*.x:443` both reduce to `*.x`.
- **Only the Host check widens.** `CrossOriginProtection` and `upgradeCheck` are unchanged. A page on another name
  under the same domain (another user's zrok share, same site) is still refused for a write (`Sec-Fetch-Site:
  same-site` is not same-origin) and for a websocket (the `Origin` host is not the `Host`).
- **The security claim holds for zrok.** A zrok user picks the share's name but not its DNS. `*.shares.zrok.io`
  resolves to the frontend, so no page can point a name under it at `127.0.0.1` or at this machine.
- The wildcard applies to every listener, loopback included, which is the same as an exact `ATRIUM_HOSTS` name today.

## Findings

### Low

1. **The bare-label guard lets a public suffix through, and the safety claim does not hold for every domain.** The
   code ignores `*.com` but accepts `*.co.uk`, `*.github.io` and `*.duckdns.org` (checked: `evil.co.uk` answers
   200). The comment says "nobody but the frontend's operator controls names under its domain". That is false for a
   dynamic DNS domain, where any user sets the A record. There, `*.duckdns.org` reopens DNS rebinding on every
   listener, loopback included. The operator has to opt in, so this is a low. Could the comment and the refusal
   message say that the wildcard is safe only for a domain whose DNS records one operator alone sets, and name
   dynamic DNS as the case where it is not? A `golang.org/x/net/publicsuffix` check would refuse `*.co.uk`, but it
   would not refuse `*.duckdns.org`, so the doc line is the fix that matters.
2. **An ignored entry is silent.** `*.com` or `*.` is dropped with no log line. The board then answers 403 with
   "$ATRIUM_HOSTS adds one", which points at the variable the operator already set. Should Named log once that it
   ignored the entry?

### Nit

- A fully qualified Host with a trailing dot (`a.shares.zrok.io.`) is refused. It fails closed, and exact names
  already behave the same way, so this is not new.

HUB DEPLOY OK e25a2c2f
