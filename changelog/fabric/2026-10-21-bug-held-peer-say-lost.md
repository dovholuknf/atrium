A say held behind a closed typing gate when the room restarts is no longer left unseen. The held set lives in memory, so
a reattached idle runner had queued says and nothing typing them, and it makes no hook call to carry them. A reattach now
puts every pending row back on the retry, and they type once, in order, when the gate opens. bug-held-peer-say-lost
