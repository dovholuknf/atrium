A worker whose work landed is reclaimed on its own. Landed now means every patch is on claude/main, the hub's copy or
origin/main, by ancestry or patch id, so a rebased branch counts. `merged_cull_grace` defaults to a day (`off` opts a room
out), and a landed worker idle at needs-input or done for that long is asked to leave, its worktree removed only when
clean and its branch pruned. A tag, a hold, an open question or a dirty worktree keeps it, and the Terminals row shows a
`landed` chip saying why. `atrium_close` on a launched worker now lists and removes its worktree even when its inventory
does not name it (b-landed-workers-never-reclaimed).
