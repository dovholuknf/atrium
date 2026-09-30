- The live deploy scripts (`deploy-batch.ps1`, `deploy-hub-only.ps1`, `start-atrium-hub.ps1`) start the hub with
  `ATRIUM_HOSTS` from the User environment rather than the caller's, and log the value in the deploy log and
  `hub.err`. A deploy from a session without it had brought the hub up without the zrok share's hosts. (live-hub-hosts)
