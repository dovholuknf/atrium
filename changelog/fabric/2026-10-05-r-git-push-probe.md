- **A push over 1 MB to the hub no longer fails with HTTP 400.** See `docs/backlog-2.md` item r-git-push-probe.

  Before a large body git sends a probe, a POST to git-receive-pack holding one flush packet (`0000`), and needs a 200. The receiver refused it as "not a git push". It now answers 200 with an empty body after the usual token, setting and caller checks, and does nothing else: no git, no lock, no push log row, no new repository. A real push with no updates is still refused.
