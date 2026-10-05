# r-context-nudge-handoff

contextLine (internal/daemon/contextnudge.go) returns "" while holdingMessages(t.ID), so capture and wake turns are never nudged and the claim is not taken. Auto path: nctx.beginAuto runs before autoPrepare types anything, so holding already covers it; no further gap. Permission chain order unchanged.

Test: TestACardInANewContextCycleIsNotToldItsContext. `go test ./internal/daemon/ -run 'Context|NewContext'` passes; build ok.
