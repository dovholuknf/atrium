## Test plan

## @LETTER@. Launch caps, control audit lines and the asked-input notice

### @LETTER@1. Launch caps refuse a case-duplicate room and an unknown field

1. `env -u ATRIUM_LOCATION go test -run TestTheLaunchCaps ./internal/link` passes.
2. On a hub, `PUT /_hub/launch-caps` with `{"rooms":{"SG3":1,"sg3":50}}` answers 400, and so does `{"room":{"sg3":5}}`.
   `GET /_hub/launch-caps` afterwards shows the caps as they were.

### @LETTER@2. The control audit line is bounded and has one line

1. `env -u ATRIUM_LOCATION go test -run TestAuditDetailBoundsAndStripsCallerText ./internal/link` passes. It fails if
   `auditWhat` is taken out of `auditDetail`.
2. Call `atrium_alias` with a long alias that holds a newline. The audit row for it is one line of at most about 300
   characters.

### @LETTER@3. A card that asked is notified as input

1. `env -u ATRIUM_LOCATION go test -run TestNotifyIdentityPerReasonAndPriority ./internal/link` passes. It holds a
   needs-input card with a seen row, no turn end and `waiting_reason` asked, which gives an `input` notice, and the same
   card with an empty reason, which is still dropped.
