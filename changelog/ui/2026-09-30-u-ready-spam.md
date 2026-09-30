A ready alert rings once per wait. It is keyed on the card and its `waiting_since`, so a card that drops out of the
waiting set for a moment and returns no longer rings again.
It also waits until the card has been quiet for 5 seconds and is still waiting. Task events and terminal output
for the attached card restart the quiet, in the board and in a pop-out.
A focused window showing the card's terminal says nothing about its ready, only the toast log line.
Permission alerts are unchanged.
