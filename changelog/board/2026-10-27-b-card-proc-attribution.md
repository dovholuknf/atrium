With the pty host on, each runner's job object is named `atrium <card title>@<room>`, so Process Explorer's Job tab
says which card owns a process, and every runner and shell carries `ATRIUM_CARD=<card title>@<room>`. The pty host
started through WMI no longer sits under a `cmd.exe`. Item b-card-proc-attribution.
