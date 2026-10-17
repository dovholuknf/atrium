- **The pty host runs as `atrium-pty`.** Its process is named for its role in Task Manager and `ps`. The copy is
  refreshed by content, not size and time, and never replaced under a running host. The old `atrium.ptyhost` copy is
  deleted once nothing holds it. Hooks and settings still point at `atrium`. (f-process-names, phase 1)
