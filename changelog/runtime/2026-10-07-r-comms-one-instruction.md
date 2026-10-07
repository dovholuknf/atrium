A launched worker hears one instruction for the end of a turn: tell your launcher with `atrium_say`, `done <sha>` or
`blocked: <one line>`. The launch prompt's last line, the room's silent-stop nudge and the lean worker prompt all say
it, and none names `atrium_report` any more. A launcher's `atrium_say` with `kind: fyi` ("stop, nothing else to do")
asks for no reply, so the worker owes none: no nudge and no "ended its turn without reporting" notice follow it. The
kind now rides every path, typed, queued, `atrium tell`, cross-room through the hub, and held for a room that is down.
