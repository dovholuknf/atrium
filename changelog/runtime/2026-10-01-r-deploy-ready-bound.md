# r-deploy-ready-bound: GET /_hub/deploy-ready always answers in seconds

The endpoint hung on the live hub, so the deploy pill never appeared. Three causes, all in the cold path:

- One lock was held across the whole git pass, which was allowed 60 s, so every caller queued behind it.
- Each merge in a range cost its own `git show --remerge-diff` and patch-id process, and each verdict range cost about
  nine git calls. At roughly 80 ms a call on Windows, a cold pass over the live history took well over a minute.
- A read cut off by a deadline could be cached as "this merge carries nothing".

What changed:

- The pass is bounded at 5 s and git is killed there. On the bound the endpoint answers `unknown` with a note saying it
  timed out, never an error and never a hang. What git did return is kept, so the next ask starts further along.
- One pass in flight and no lock held across it. A caller that arrives meanwhile gets the last answer with `stale: true`,
  or `unknown` if there is none, at once. A deploy click during a read gets a 409 rather than acting on a stale answer.
- Merges in a range are read together in two git calls. A verdict range costs one call once its commits are cached, and
  none once the range itself is cached (ranges named only by commits are immutable).
- A range is limited to 2000 commits. Longer than that answers `unknown` and says so.

Review follow-up:

- A merge read that fails for any reason but an old git now fails the pass and caches nothing. Before, it cached every
  merge in the batch as "carries nothing", so a conflict resolution needed no verdict and the answer was ready for the
  life of the process. Only a merge git listed is cached, and only an unknown `--remerge-diff` option counts as old git.
- A pass started by a GET now tells watching boards when the answer moves, so a pill does not sit on unknown until the
  minute tick.
- A verdict range cut at the commit limit says so in the answer's notes.

On this repo, 150 commits behind with 31 verdict ranges: the third ask answers in about 0.6 s, the second in about
2.6 s, and the first in 5 s with `unknown`.
