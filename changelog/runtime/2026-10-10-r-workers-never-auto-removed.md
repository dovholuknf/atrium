A worker is never culled because its branch merged: `merged_cull_grace` defaults to `off` in code, so a new room or a
reset setting keeps it off. Only an explicit `atrium_cull` or `atrium_exit` removes a worker
(r-workers-never-auto-removed).
