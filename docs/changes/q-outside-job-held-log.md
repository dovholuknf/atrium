# Outside-job start: hold retries access denied

`startOutsideJob` held the new pid with one `os.FindProcess`. CI run 39814909117 saw `OpenProcess: Access is denied`
for a live PING.EXE just created. `holdProcess` retries only that error, up to 20 times at 25 ms.

## Test plan

- `go test -tags integration ./internal/detach -run StartOutsideJob` on Windows passes.
