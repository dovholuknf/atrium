# Interview: how atrium work goes from "start it" to running on every room (2026-10-09)

## False start (thrown out, restarted at clint's request)

- First Q1 asked "which steps should change, who does each, where does done end". clint: "that's the worst interview question ever. it's like 'how to draw an owl'". Not answered.
- Then Q2 asked who moves claude/main forward. clint: "q1 is 'not answered' ... you asked me 'what changes' which was a gigantic question... start this interview all over, state to me the goal first". Not answered.

## Q1: is this the goal?

Answer (to "what is the summary?" then the question): "to be honest i think we have lost the whole plot here. i think i was talking about how workers get jobs, do jobs, ack jobs, then push to hub then clint merges/pushes to his repo but i don't now"

## Q2: is this your picture of the flow?

Asked: 1. worker gets a job. 2. does it on its own branch. 3. reports done. 4. pushes its branch to the hub. 5. clint merges and pushes to dovholuknf/atrium. Default: yes, only clint merges.

Answer: "not quite. clint tells dispatch/orchestrator "fix bug" . orchestrator finds room and says "fix bug".  room pulls code if it needs to from hub, room makes worktree, room tries to fix bug. if room gets blocked, room calls atrium blocked tool and waits for further instruction. atrium blocked takes up to 50 words. omre words the agent should leave a doc behind with a set of questons and point where that is. if not blocked and if done anget juust pushes into the hub with branch, replies "hey u, room1@job-name is done with sha xyz" that's it. then it sits there dormant.

orchestrarot gets message and works on merging branch to 'main' and then once a 'sizeable chunk of changes' have happenes runs tests. tests fail then we find where the test failed and how, agent that did the work pulls/merges and finds the issue rinse repeat.

that make sense?"

## Q3: who deploys, and when?

Asked: four branches merged into main, tests pass, rooms still on the old build, and a deploy restarts rooms. (a) orchestrator deploys right away, (b) orchestrator asks clint first, (c) wait until clint has pushed to dovholuknf/atrium. Default (b).

Answer: "d: after "enough" work lands -- which is fluid -- or after ALL work is done then "tests run" and if no work is on a room - deploy immediately. if work is on the room we wait. if more work lands re-eval if tests need to be run, if not just deploy room"

## Q4: who fixes a merge conflict?

Asked: the peek branch, started from an old main, no longer merges cleanly. (a) orchestrator fixes it, (b) sent back to the worker to merge main and fix, (c) orchestrator fixes small ones and sends the rest back. Default (b).

Answer: "b seems the best but the problem is that more work can't be getting accepted so really i think it's "the merge guy" and 'the merge guy' is not the orchestrator. in fact. maybe the flow should be orchestrator->worker-> merge guy->orchestrator after merge is complete?"

## Q5: what does the merger own?

Asked: (a) merge, conflicts, tests, sending failures back and deploys, or (b) only the merge, with the orchestrator still running tests and deploys. Default (a).

clint asked first: "thoughts on q4's answer?" I said: a separate merger is right. Sending a branch back stalls only that branch, so the merger fixes simple conflicts and sends back real ones. The merger must be able to move main (hooks, main checkout). One merger with one queue, so slow tests are the bottleneck.

Answer: "these all feel like discrete steps that should be captured by atrium along with 'who is the orchestrator', 'who is the merger', 'who is running tests' so that if the system restarts the state can be rehydrated from atrium itself and not from agents fucking about talking to one another. this would allow also a clean tracking of 'backlog items' with state of where they are at from 'idea' to 'designed' to 'implementing' ot 'implemented' to 'merged' to 'tested' to 'deployed' i think and also 'did clint merge to the main/live/real git repo'?

i'm not sure if merger owns 4 or not tbh. it's plausible that varioius chunks might be able to be doled out to different testers. for example testing UI on sg3 but go on m1mini?"

## Q6: who hands out the test runs?

Asked: five branches merged, tests split UI on sg3 and Go on m1mini, the Go tests fail. (a) the merger hands out the test chunks and routes failures, (b) the orchestrator does, (c) atrium starts them itself. Default (a).

Answer: "provide feedback on 5 as well and on the rest as we go. if anything is sus - make sure you ask/comment.

a. the merger can do it for now. it might be the merger, it might not. i think if we keep this linear it'll give us options"

My feedback on Q5's answer (state kept in atrium):
- Strongest idea so far. It removes the restart problem and the "agents talking to each other" problem at once.
- Open: who moves an item from one state to the next. Agents forget tool calls, so atrium should read states from git where it can: a branch on the hub means implemented, main containing the sha means merged, origin/main containing it means clint pushed. Only "designed", "tested" and "deployed" need someone to say so.
- "tested" belongs to a sha of main, not to one item. An item is tested when a test run passes on a main that contains it.
- "deployed" is per room. An item can be live on sg4 while sg3 waits for its sessions to go idle.
- Not every item needs "designed". A small bug fix skips it.

## Q7: where does the merger move main from?

Asked: (a) a session started in the main checkout on sg4, (b) the merger works in its own worktree and asks atrium to move claude/main once atrium has checked the role and that it's a fast-forward, (c) loosen the hooks for the merger role. Default (b).

Answer: "i think b but really the 'merger' owns 'claude/main' in my head. then when user (clint) is ready to merge, we will want some kind of flow where clint has an github:orign/main and a hub:claude/main and clint will pull/fetch hub:claude/main and then do a merge/resign/push"

My feedback: "the merger owns claude/main" fits (b) if atrium enforces it: only the recorded merger can move claude/main. The flag is the re-sign. Re-signing rewrites every sha, so origin/main and claude/main hold the same code under different shas. Then:
- atrium can't tell "did clint push it" from git
- the next round, claude/main has to be reset onto origin/main
- every worker branch still in flight is now based on a main that no longer exists, which is the 2026-10-09 cherry-pick pain again

## Q8: after clint pushes, what happens to claude/main?

Asked: (a) re-sign every commit, then reset claude/main to origin/main and the merger rebases in-flight branches, (b) one signed merge commit with the worker shas kept and claude/main fast-forwarded, (c) re-sign and never reset, matching commits by content. Default (b), unless GitHub requires every commit signed.

Answer: "seems like when i resign it should force claude/main to just reset from origin no?"

My feedback: yes, with (a) a reset is the right follow-up. I also overstated the damage in Q7. Re-signed commits have the same content, so `git rebase origin/main` drops the duplicates and in-flight worker branches mostly rebase cleanly. What's actually risky is the gap: anything the merger lands on claude/main between clint's fetch and clint's push is wiped out by a plain reset.

## Q9: what about work merged while clint is pushing?

Asked: clint fetches abc at 10:00, the merger lands def at 10:05, clint pushes re-signed abc at 10:10. (a) atrium freezes claude/main between fetch and push, (b) the merger resets and puts back what came after abc, (c) reset only if still at abc, otherwise sort it out by hand. Default (b).

Answer: "q8: or does it make sense to make a claude/main-date type of branch and then all work starts from that new sha? maybe that needs more thought. i don't really know what we are doing 'today/lately' but that seems to be wroking?

q9: maybe we need to 'figure it out' i dunno"

What git shows today (fetched from the hub during the interview):
- origin/main's commits are signed. hub claude/main is origin/main plus one unsigned commit.
- `git fetch hub` reported claude/main as a forced update (bf68faa5 → 229a921d). So today claude/main is already being reset onto clint's re-signed origin/main, and new work goes on top.
- The hub also has claude/base-* branches, which are close to the dated-branch idea.
- The hub has about 70 claude/* branches and the room mirrors have hundreds. Nothing cleans them up.

My feedback: a dated branch (claude/main-2026-10-09) never moves backwards, so nothing gets force-pushed, and branches in flight still point at a base that exists. The cost: every worker and tool has to know today's name. If atrium records "current main is claude/main-2026-10-09" as state (decision 13), that cost goes away. Q9 is left open for the R&D agent, with one rule: no merge is ever lost.

## Q10: which roles are left?

Asked: clint, orchestrator, worker (a tester is a worker with a test job), merger. (a) those four, with directors and land-and-deploy ops workers gone, (b) keep directors, or another role. Default (a).

Answer: "well bringing directors back MIGHT work as long as it goes: orchestrator -> director -> worker -> merger -> tester -> deploy -> orchestrator (to wrap it all up) but we can't be blasting through tokns like before with all the fucking interaction between agents we had before, with atrium injecting 'noise' etc."

My feedback:
- Each hop is one handoff and one message, nothing more. A director should be optional per item: worth it for something that needs designing, pure cost for a one-line fix.
- The noise is real, and this interview had plenty: the automatic "your turn ended without telling your launcher" nudge (against clint's rule for this interview), the idle park that cut it off for 2 hours, the "context about to be cleared" handoff, and orchestrator messages wrapped as pasted untrusted text. If atrium holds the state (decision 13), most of these nudges can go: atrium already knows who is waiting on what.

## Q11: which tests have to pass before a deploy?

Asked: six branches landed (four UI, two Go). (a) the full suite every time, (b) only the tests for the changed parts, with a time limit, (c) (b) for each deploy plus the full suite daily or before clint pushes to origin. Default (c).

Answer, to my Q10 feedback and Q11:

> - After you'd said no atrium calls between questions, I got the automatic "your turn ended without telling your launcher" nudge anyway.
>
> we need to fucking eliminate that. it's a serious bug... and burns time/tokens when cards are just sitting there
>
> - There was a forced "context about to be cleared" handoff.
>
> this is fine this is waht i want though.
>
> - Every orchestrator message arrived wrapped as untrusted pasted text.
>
> not sure that's a good or bad thing
>
> - The director should be optional per item. It earns its keep on work that needs designing, like this merge flow.
>
> yes but it ALSO might work as a 'first merger' and 'first tester'... orchestrator -> director -> worker (n) -> director (tests/merges to 'director branch) -> main merger -> one more tester -- but also that is getting a bit complicated so maybe we'll do that later if we decide to try...
>
> q11: i don't know that ANY need to really . but -- maybe we need some __FUCKING FAST__ sanity type checks .. id' be fine with that as long as it was like "1-2 minutes" per build/deploy type of thing

My feedback on the wrapped messages: the label saying who a message is from is good. Treating the launcher's own instructions as untrusted is the bad part. It cost a round trip in this interview: the orchestrator had to say "confirmed: from clint" before its rules counted. Suggestion: instructions from your launcher, or from the orchestrator relaying clint, count as instructions. Messages from any other session stay labelled as peer context.

## Decisions

1. clint tells the orchestrator the job. The orchestrator picks a room and passes the job on.
2. The room pulls code from the hub if it needs to, makes a worktree there, and does the job.
3. Blocked: the worker calls the blocked tool (up to 50 words). If it needs more words, it leaves a doc of questions and points to it. Then it waits for instructions.
4. Done: the worker pushes its branch to the hub, sends one line ("room1@job-name is done with sha xyz"), and goes dormant.
5. The orchestrator merges the branch into main. Tests run once a sizeable chunk of changes has built up, not on every merge.
6. If the tests fail: find where and how, then send it back to the worker that did the work. That worker pulls or merges, fixes it, and the loop repeats.
7. Roles: clint, the orchestrator, and workers.
8. Tests run after "enough" work has landed (the orchestrator judges that), or after all the work is done.
9. Deploy is per room, with no ask: a room with no work running gets the deploy at once. A room with work running waits until it's idle.
10. If more work lands while a room waits, decide again whether tests need to run. If they don't, deploy the room.
11. Merging is a separate role, the merger, not the orchestrator. Proposed chain: orchestrator → worker → merger → orchestrator once the merge is done. This replaces 5 and 7: the roles are clint, the orchestrator, workers and the merger.
12. Merge conflicts: handing the branch back to the worker is right in principle, but merges can't stall waiting on it. The merger owns conflicts.
13. Atrium itself records each step and who holds each role (orchestrator, merger, tester). After a restart, the state comes back from atrium, not from agents messaging each other.
14. Each backlog item carries a state: idea → designed → implementing → implemented → merged → tested → deployed → clint pushed it to the real repo (dovholuknf/atrium).
15. Testing may split into chunks on different rooms, for example the UI tests on sg3 and the Go tests on m1mini. Whether the merger owns testing and deploys is still open.
16. For now the merger hands out test chunks to testers, collects the results and sends failures back. That may change later. Keep the chain linear, to leave options open.
17. The merger owns claude/main on the hub. It merges in its own worktree and asks atrium to move claude/main. The hooks stay strict.
18. The step to the real repo is clint's: he fetches hub:claude/main, merges, re-signs, and pushes to github:origin/main.
19. Once clint has pushed re-signed commits, claude/main is reset from origin/main. That already seems to happen today, and it works. An alternative to weigh: a dated main branch per push (claude/main-2026-10-09) that all new work starts from. "Needs more thought."
20. Open, for the R&D agent to figure out: work the merger lands between clint's fetch and clint's push. The rule is that it's never lost.
21. The chain: orchestrator → director → worker → merger → tester → deploy → orchestrator, which wraps up. Directors may come back, but only inside this linear chain.
22. Hard limit: no token-burning back-and-forth between agents, and no noise injected by atrium. Each hop is one handoff.
23. Bug to fix: the automatic "your turn ended without telling your launcher" nudge. It fires on cards that are just waiting, and burns time and tokens. Remove it.
24. Keep the "context about to be cleared, write a handoff" step. clint wants it.
25. Wrapping session messages as pasted text: undecided whether it helps or hurts.
26. Later, maybe: a director as first merger and first tester. Its n workers merge into a director branch and get tested there, then go to the main merger and one more tester. Too complicated for now.
27. Tests: maybe no gate at all. At most, very fast sanity checks, about 1-2 minutes per build or deploy. No full suites as a gate.

## Open for the R&D agent

- Reset claude/main after each push, or cut a dated main branch (19).
- Work landed between clint's fetch and push must never be lost (20).
- Which states atrium reads from git and which ones someone has to report (my Q6 feedback).
- Clean up branches on the hub and the room mirrors once an item reaches "clint pushed it". The hub has about 70 claude/* branches and the mirrors have hundreds.
- Whether the merger keeps testing and deploys, or hands them on (15, 16).
