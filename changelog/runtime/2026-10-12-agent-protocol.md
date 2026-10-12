A launched worker now ends one way: `atrium_done` with a sha or artifact and an optional line of up to 20 words, or
`atrium_blocked`. A stop is answered with `atrium_done` and the last sha, and a repeat done is silent. A card tagged
`atrium:resident` (`resident: true` on launch) hears words that leave its card open. `launcher_told` says `queued` or
`relayed`, and a card launched on a branch may push it when its launcher or an earlier sibling pushed it first.
Item: agent-protocol.
