scripts/atrium-autostart.ps1: the room logon task command quotes the exe, db and address paths so a typographic quote
in a path (macOS autocorrects the apostrophe) cannot end the string early and run what follows. Item f-new-ps-quote-sweep.
