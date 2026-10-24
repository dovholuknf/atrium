The hub reads whether items have landed and been pushed from one git log per branch tip, kept until the tip moves,
instead of starting a git process per item every pass. A hub with many open items no longer keeps a crowd of git
processes running. (bug-landfacts-git-storm)
