## Test plan

Test plan @LETTER@: growler reminders, on a hub (hub half only, the board's "remind me" button is separate).

1. `curl -s localhost:<hubport>/_hub/growl-ladder` answers `{"permission":true,"question":false,"blocked":false,"halt":true,"deploy-hold":true}`.
2. Have a card ask an Open Question. One toast and one phone push arrive. Wait past 2 minutes: no "growler reminder" toast and no further push. The question stays in the bell.
3. Have a card wait on a permission for over 2 minutes. It growls, then reminds at about 1, 2, 5 and 10 minutes after the raise, phone included.
4. Snooze the question for 1 minute with `curl -X POST localhost:<hubport>/_hub/growls/<id> -d '{"do":"snooze","minutes":1,"via":"board"}'`. About a minute later it re-raises once and the phone is pushed once, with no reminders after it.
5. `curl -X PUT localhost:<hubport>/_hub/growl-ladder -d '{"question":true}'` then raise a new question: it now reminds on the ladder. PUT `{"question":false}` and it stops on the next 30 second tick.
6. A PUT through a proxy or from another machine answers 403. A reason that is not one of permission, question, blocked, halt, deploy-hold answers 400.
7. Let a card with an open question end (finish or exit it so it is done or dead): its growler leaves the bell on the next tick, snoozed ones too.
