# Every share on api-v2.zrok.io returns 500 with an empty body

A filable report. Nothing here is an atrium defect, and it is written to be pasted into an issue against
`openziti/zrok` rather than to be read as a design note. It is in this repository because it is the reason the
reserved-share work is built and unproven, and because whoever picks that up next needs to know it is not
theirs to fix.

Measured 2026-09-06 against `https://api-v2.zrok.io`, zrok 2.0.4 client and generated REST client.

## The symptom

Every `POST /api/v2/share` answers `500` with a body of `""`. Public and private, every backend mode, with and
without a name selection. The `zrok` CLI fails the same way, because it is the same call.

```
HTTP/1.1 500 Internal Server Error
Content-Type: application/zrok.v1+json
Content-Length: 3

""
```

## What still works, which is what makes this specific

The account is fine and the controller is mostly fine. Each of these was measured with the same token, in the
same minute, against the same instance:

| request | answer |
| --- | --- |
| `GET /overview` | `200`, four environments, three shares, thirty five names |
| `POST /share/name` for a free name | `201` |
| `DELETE /share/name` for that name | `200` |
| `POST /share` with an `envZId` belonging to another account | `401` |
| `POST /share` with a bad token | `401` |
| `POST /share` with `shareMode: "sideways"` | `422`, with the validation message |
| `POST /share`, `privateShareToken: "!!!"` | `409` `requested private share token '!!!' has invalid unique name` |
| `POST /share`, `privateShareToken` of an existing service | `409` `service name 'openziti-mc' is already in use` |
| `POST /share`, `privateShareToken` of a free name | **`500`, empty body** |
| `POST /share`, public, with a name selection | **`500`, empty body** |
| `POST /share`, public, with no name selections at all | **`500`, empty body** |

## Where that puts the failure

Reading `controller/share.go` in 2.0.4, the handler is a sequence and each step has its own status. The
measurements above walk it:

1. `validateEnvironment` refuses with `401`. Reached and passed, because a foreign `envZId` gets the `401` and
   ours does not.
2. `checkLimits` refuses with `401`. Passed, same reasoning.
3. `createShareToken`, then for a private share `checkPrivateShareTokenAvailability`, which refuses with `409`.
   **Reached and answered correctly**, and this is the interesting one: that check is
   `automation.NewZitiAutomation(cfg.Ziti)` followed by `ziti.Services.GetByName`. It correctly reported
   `service name 'openziti-mc' is already in use`, so **the zrok controller authenticates against its ziti
   controller and reads from it successfully.**
4. `allocatePrivateResources` / `allocatePublicResources`, whose first act is `ziti.Configs.Create`. Everything
   that reaches here returns `500`.

Public shares with zero name selections skip name handling entirely and still fail, so nothing in the
namespace or name path is involved.

**So: reads against the ziti controller succeed, and something inside resource allocation fails.** Allocation
opens with `ziti.Configs.Create`, so that call is the first candidate, but a client cannot see which step
inside allocation gave up: every one of them collapses into the same empty 500. The obvious causes are the zrok
controller's ziti identity having lost create permission, or the ziti controller refusing the create for its
own reason, and neither is visible from out here either.

## The second, smaller defect: the 500 carries no payload

`share.NewShareInternalServerError()` is returned with no `.WithPayload(...)` at all eight places it appears in
`controller/share.go`, so the body is the zero `errorMessage`, which serialises as `""`. The spec declares an
`errorMessage` schema for the 500 on this operation, and the handler's `409`s do carry a sentence saying what
conflicted, so the pattern is already there to follow.

The result is that a client can say nothing at all about the most common failure on the endpoint. Passing
`err.Error()` the way the conflict paths already do would have made this report one line instead of a
bisection.

## Reproducing it

Any enabled environment on the instance. `ENVZID` is the `ziti_identity` in `~/.zrok2/environment.json`, which
is the environment's `zId`.

```bash
TOK=$(jq -r .zrok_token ~/.zrok2/environment.json)
curl -si -X POST -H "x-token: $TOK" -H 'content-type: application/zrok.v1+json' \
  -d "{\"envZId\":\"$ENVZID\",\"shareMode\":\"private\",\"backendMode\":\"proxy\",\"target\":\"localhost:9999\",\"permissionMode\":\"open\"}" \
  https://api-v2.zrok.io/api/v2/share
```

What would settle it, and needs an operator of the instance rather than a client: the zrok controller log
around one of these requests. The handler logs `error allocating share resources: %v` at the point it gives up,
and that line holds the whole answer.
