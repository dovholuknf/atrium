---
title: Messages and peers
description: Saying something to a running session, actions you press on any card, and sessions talking to each other.
---

# Messages and peers

There is no way to type into a claude process from outside, and atrium does not invent one. A message waits in the
card's queue until the session can hear it.

## Saying something to a session

Type into the message box on a card. The message is delivered the next time the session can hear one:

- **Typed into its terminal**, when atrium owns the terminal, your line is empty and the keyboard is quiet.
- **Carried by a hook**, on the session's next tool call through the permission gate, or at the end of its turn
  through the optional `Stop` hook.

It arrives framed as a message from you, not as a policy refusal. Saying anything to a card also answers the
question it had open.

## Actions

An **action** is a prompt you write once and press on any card. Find them on the **rooms** tab. Limit one to a tag
or a runner when it only makes sense there.

An action can ask the runner to quit afterwards: atrium sends the prompt, then the runner's own exit keys. **write
it up and finish** is there to begin with. It tells a session to write up what it did and finish.

## An agent telling atrium things

A few commands exist because atrium cannot infer them. They are commands, not MCP tools, because a command is the
one channel every runner has.

```bash
atrium finish "bumped the dependency, ran the tests, opened a pull request"
```

The card moves to **finished** and keeps that sentence. `--hand-back` moves it to **ready** instead: handing the
work over without claiming it is over.

```bash
atrium ask "which of these two schemas is authoritative"
atrium ask --continue "is the staging database safe to drop"
```

The question goes on the card as **this agent has a question**. Without `--continue`, the session is saying it has
stopped, and the card moves to ready. With it, the session carries on and the card stays where it is. You can take
a question off a card without telling the session.

## Sessions talking to each other

Sessions find each other and talk through atrium. Nothing is typed into a line somebody is writing.

```bash
atrium peers                                           # the handles this session can address
atrium tell api/fix-login "the schema change landed"   # say something
atrium ask --peer docs/release-notes "which branch is base for the notes"
atrium answer api/fix-login "base is main"             # reply, which also clears the question
```

A peer message is typed into the target's terminal when that terminal is free, and queued and retried when it is
not, from two seconds out to four hours. A handle nobody has is refused with the handles that would have worked.
Peer messages are capped at 8,000 characters and 20 a minute.

A session blocked on a peer still shows as waiting, and the card names the peer. That is on purpose: a peer that
never answers otherwise looks like a session nobody noticed.

## A session that stops without a word

A session another session launched may stop without saying anything. Atrium tells the launcher, then the board, on
a backoff from one minute out to a day. It never forces a turn to find out why: stopped sessions cost nothing, and
atrium keeps it that way.
