# Interviewer brief: the template for a card that interviews clint on a design, and how questions are phrased

Status: a standing template kept by @rnd, first written 2026-10-02 from the hub-forge interview (card 01a0fdcf on
sg4-control). Copy section 2 into an interviewer's brief. Sections 3 and 4 also apply to every "Questions for
clint" section in a design doc.

## 1. What went wrong the first time, and what worked

- **The questions were not understood.** clint's first answers were complaints: the wording was back to front, and he did not understand the question at all. It worked once a question was rephrased as a
  sequence: this happens, then this happens, then what?
- **The design's core picture was wrong, and the interview only found out at question 4.** The design had the hub
  mirroring every repo. clint's picture was a hub that owns `main`, takes pushes, and routes everything else to the
  room that has it. Three questions were spent on the details of a picture he did not hold.
- **A question was asked that he had already answered.** He had already said, in passing, that a clone keeps both
  remotes, and was asked again. He pointed out it had been asked before.
- **The process itself worked.** One question at a time, a default each, and an answers file written as he went.
  He asked for a better screen for it (`docs/rnd/operator-focus.md` section 2.9).

## 2. The brief (copy this into the interviewer's BRIEF.md)

> You are interviewing clint on `<design doc>`. Your job is to find out how he pictures it working, then the
> mechanisms, then the edge cases, in that order, and to write his answers into `<answers file>` as you go.
>
> 1. **Ask for his picture first, as a concrete scenario with real names.** Question 1 is always the whole flow, for
>    example: "sg4 has finished `fix/x`. m1mini needs to review it. Walk me through what happens, step by step."
>    Use real machines, real branches and real repos. Before question 2, write back his picture in five lines and ask
>    whether it is right. If it differs from the design's picture, say so plainly and continue from **his** picture.
>    The design is rewritten afterwards.
> 2. **Then the mechanisms, then the edge cases.** Do not ask about an edge case of a mechanism he has not agreed to.
> 3. **Phrase every question as a sequence.** "This happens. Then this happens. Then what?" Short sentences, active
>    voice, the subject first. No internal names: say what a thing does before naming it, or do not name it.
> 4. **One question at a time.** Give two to four options, each saying what would happen. Mark one as the default,
>    with one line of why. Free text is always allowed.
> 5. **Never re-ask what he already said.** Keep an "already said" list in the answers file. Before each question,
>    check it. If he said it in passing, write it down as "taken as: ..." and move on. If it is unclear, ask to
>    confirm it in one line, not as a fresh question.
> 6. **When he does not understand, rephrase it as a scenario.** Do not repeat it or explain the mechanism. Count the
>    rephrases in the answers file.
> 7. **Write the answers as you go.** For each question, record his exact words, then "taken as: ..." for your
>    reading of them, marked as yours. The file stays outside any public repo (on the hub machine, or wherever the
>    factory log is kept), because it quotes him. Your own cwd is never a public repo's worktree, so nothing you
>    write can be committed there by accident.
> 8. **Stop when the picture is settled.** List what was not asked under "Open, for the designer to default". Do not
>    ask about them just to be complete.
> 9. **When done,** report to whoever launched you with the path of the answers file and three headline lines, then
>    exit.

## 3. Phrasing a question for clint, anywhere

The same rules, for a design doc's "Questions for clint" section and for anything held in his decision list:
- **A scenario, not a mechanism.** "sg4 is asleep. m1mini asks for sg4's `fix/x`. Should it fail, or get a copy the
  hub kept?" Not: "should the hub keep a fallback mirror of room refs?"
- **Subject first, active voice, short sentences.**
- **Real names:** machines, branches, repos, cards.
- **One question, a default, and why.**
- **Recap the item in one line** for a reader who has forgotten it.
- **Check the answers already on file first** (the interview's answers file, the decision list, the interview log)
  and never ask one again.

## 4. Order in a design doc's questions

Put the question that tests the design's core picture first, and its edge cases last. If the first answer could
change the design's shape, the doc says so beside the question ("if no, sections 2 to 4 change").
