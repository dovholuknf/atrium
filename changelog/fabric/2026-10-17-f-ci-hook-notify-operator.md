Every post the hub's CI hook receives now raises one board alert saying what came in (repo, workflow, branch, short sha,
conclusion, attempt, run link) and what the hub did with it: routed to which sessions, or dropped and why (no route,
filtered, bad or missing secret, unlisted repository, malformed body, repeat). `f-ci-hook-notify-operator`
