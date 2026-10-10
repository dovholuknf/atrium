package gitsync

import ()

// ── the lookup against rooms that answer from a function ───────────────────────────────────────────────────

func room(name string) RoomInfo { return RoomInfo{Name: name, Git: true} }
