# An edit that does not parse is a deny

When the human edited a raw-JSON tool summary and the edit did not parse, the permission hook sent a plain allow and the original call ran. It now denies with "the edit did not parse, nothing was run".
