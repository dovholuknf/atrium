`room-git.ps1 push-base` and `init` refuse a `-From` other than `claude/main` unless `-Force` is given, and say
why: hub-main on a room mirrors claude/main only, since every worker there branches from it. (hub-main-mirror)
