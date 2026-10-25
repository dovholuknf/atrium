A room whose every hub address fails now finds its hub again on its own. A hostname is asked of DNS afresh when the
cached answer does not answer, and room.json and transports.json are read again before each attempt, so `atrium room
hub <addr>` takes effect with no restart. The room's board says it cannot reach the hub at any known address and names
each address it tried. bug-room-stale-hub-address.
