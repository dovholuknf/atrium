# The repos page opens at once

`GET /_hub/git/repos` took 2-12s on sg4 because it ran a git process per repository on every request. It now answers
from a snapshot the hub rebuilds in the background (after a push, after an init, every 20s) and says how old it is
(`computed`, `age_seconds`, `building`). The page draws what it gets and asks again if the first build is not done.
