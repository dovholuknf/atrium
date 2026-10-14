The Stop hook can now block a reply that hands you a deploy command in a code block (`reply-hands-deploy-command`). It
needs `ATRIUM_GUARD=on` and the Stop hook registered, fails open, and blocks at most once a turn. r-rules-ask.
