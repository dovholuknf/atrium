A deploy no longer ends every runner by itself when the room refuses a stop that would take its pty host down: it
stops, says how many runners are held, and ends them only with `-KillRunners`. A pty host started under a job that
refuses breakaway is now started outside it. r-bug-deploy-kills-all-runners.
