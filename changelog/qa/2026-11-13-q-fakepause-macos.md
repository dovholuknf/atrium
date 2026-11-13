# fakePause on macOS

`fakePause` in the headless board helpers now sizes its pause margin from a measured page round trip, so paste units
no longer throw "the page clock outran every margin" on a starved macOS runner. Inferred, not yet run on macOS.
