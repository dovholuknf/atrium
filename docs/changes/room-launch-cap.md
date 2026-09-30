## Test plan

### @LETTER@. The launch cap per room (room-launch-cap)

1. On the hub: `curl -s http://127.0.0.1:7778/_hub/launch-caps` answers `{"default":10,"rooms":{}}` before anything
   is set.
2. `curl -s -X PUT -H 'Content-Type: application/json' -d '{"default":5,"rooms":{"claude-sg4":10,"sg3":5,"m1mini":5}}'
   http://127.0.0.1:7778/_hub/launch-caps` answers the same caps back. A GET after a hub restart still does.
3. A PUT with a cap of -1 or 1000, a blank room name, or not JSON answers 400 and changes nothing. A PUT from another
   machine, through the board's overlay address, answers 403, and a GET from there answers the caps.
4. With sg3 at 5 live `atrium:subagent` workers, `atrium_launch room=sg3` is refused with "at the launch cap of 5
   running workers on room sg3", and a launch onto claude-sg4 with fewer than 10 there goes through.
5. Directors and parked workers (no live terminal) do not count on either room.
