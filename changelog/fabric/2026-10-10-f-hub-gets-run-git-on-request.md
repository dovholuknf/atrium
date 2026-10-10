# The hub's pushed and deploy-ready reads no longer wait on git

`GET /_hub/git/pushed` and `GET /_hub/deploy-ready` ran git while the request waited. They now answer at once from a
snapshot the hub rebuilds in the background (after a push, after an init, on a timer), and say how old it is
(`computed`, `age_seconds`, `building`). The first ask answers `building` with an empty answer. The deploy click still
reads git fresh. Item f-hub-gets-run-git-on-request.
