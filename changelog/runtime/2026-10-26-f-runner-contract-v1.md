Runner contract v1 is published in `docs/runtime/runner-contract.md` with a JSON Schema per call. Every hook body may
carry `contract`, `/session` start may carry `features`, and `GET /contract` lists the versions a room speaks.
`atrium adapter test` runs a conformance kit against an adapter, with a fake room that answers, refuses, hangs and
sends garbage. (f-runner-contract-v1)
