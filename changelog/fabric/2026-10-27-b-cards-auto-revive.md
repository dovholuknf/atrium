A room now marks a card live in its database the moment the card's runner starts, and no longer keeps `reopen.json`.
After a reboot, an OOM or a crash, the room brings back every card that was live, including cards started since the
last clean stop. A card closed on purpose stays closed. The first start imports and removes an old `reopen.json`.
(b-cards-auto-revive)
Ctrl-K with nothing typed now lists cards whose process died while they were live, marked "killed", under a "revive
all N" row that resumes them in one step. (b-cards-auto-revive)
