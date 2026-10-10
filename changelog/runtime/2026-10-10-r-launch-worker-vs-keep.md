New control tool `atrium_launch_worker` starts parallel work marked `lifetime: work`: atrium exits its runner once its
branch merges, even with `merged_cull_grace` off. The card and the worktree stay. `atrium_launch` is unchanged and its
cards are never exited on a merge. `merged_cull_grace` now marks and reclaims work cards only. The card details show
the lifetime (r-launch-worker-vs-keep).
