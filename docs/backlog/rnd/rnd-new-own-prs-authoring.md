# rnd-new-own-prs-authoring. clint's own PRs, the authoring half

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G8 of docs-deps. Owned by @rnd.

## What is missing

Everything filed reviews other people's PRs. Nothing covers:

- opening a PR from atrium work (agents may not push, so the hand-off must be clint's)
- answering review comments on clint's openziti PRs
- CI going red on a PR clint opened

The far-backlog PR and CI item is parked and read-only.

## Why it is needed

Most of clint's PR time is on his own PRs, and a release goes through PRs.

## Depends on it

- PR and CI state on the card (A18)
- release 0.0.1

## Done looks like

- A short design that keeps the rule that agents never push. It says what atrium prepares (branch, title, body, the
  `gh pr create` line for clint to run) and what clint does.
- How a review comment on clint's PR becomes a card or a finding to answer, and how the answer is posted.
- How a red CI run reaches the card and what it offers (open the log, re-run, start a fix).
- Which parts need the forge interface and the lifecycle items, and which stand alone.
- Design only.

## Contribution policy gate (clint, 2026-10-02)



clint: "check their committer policy - in fact that should be just part of the factory's edit - make sure llm

contributions are allowed, cla's are needed all that sort of needful before i put a pr up".



Every PR to a repository clint does not own passes a policy gate BEFORE the build starts, so a NO-GO costs nothing:

AI or LLM contribution policy (allowed, forbidden, must disclose), CLA (which, how signed, employer impact), DCO

sign-off, issue-first or maintainer-OK rules, commit and PR conventions, changesets, test and lint requirements, and

the commit author identity the PR must carry. Output is a GO, GO-WITH-STEPS or NO-GO verdict with sources quoted, and

the steps clint must take himself. The first run is the OpenWiki factory test (f-new-openwiki-zrok, @fabric).
