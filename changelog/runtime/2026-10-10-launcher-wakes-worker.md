A plain `atrium_say` from a launcher to its own worker that has already finished now resumes that worker and delivers the
words, as a say to a parked family card already did. Before, the worker was exited a few seconds after it reported done,
so the say was only kept on its card and the worker sat dead unless the launcher passed `wake`. A sender that is not the
worker's launcher still has its say kept without a wake. The `wake` description says a launcher need not pass it for its
own worker. Item r-agent-loop.
