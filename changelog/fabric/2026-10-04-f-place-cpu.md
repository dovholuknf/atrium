A room now tells its hub its idle CPU on the heartbeat it already sends (`idle_cpu`, a percent over the few seconds
since the last beat), and PR placement breaks a tie on running sessions by the most idle CPU before the room name. A
room that sends no figure, an older build or a machine that cannot read one, sorts after rooms that do. Windows and
Linux read the CPU tick counters. macOS has none without cgo, so it sends an estimate from the one minute load average
over the CPU count. Nothing new is dialled and nothing new polls.
