A room now marks a card live in its database the moment the card's runner starts, and no longer keeps `reopen.json`.
After a reboot, an OOM or a crash, the room brings back every card that was live, including cards started since the
last clean stop. A card closed on purpose stays closed. The first start imports and removes an old `reopen.json`.
(b-cards-auto-revive)
Ctrl-K with nothing typed now lists cards whose process died while they were live, marked "killed", under a "revive
all N" row that resumes them in one step. (b-cards-auto-revive)
A `claude` started inside a card's terminal can no longer end that card, even when it reports before the card's own
runner does. A card a failure killed stays on the board and is never archived as dead. (b-cards-auto-revive)
A session started by hand with `claude --resume` of a card's conversation lands on that card. One started in a folder
whose only card is finished gets a card of its own, where before it had none. (b-cards-auto-revive)
