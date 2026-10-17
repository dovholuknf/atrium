- A pty host or room started from a job object that refuses breakaway is now really started outside it. The WMI path
  handed cmd.exe a log the starter still held open, so cmd failed at once and the process was started inside the job
  after a full poll (7 s on the Windows runner, 20 s and more on a desktop). The log goes by path now, the console WMI
  gives cmd is hidden, and the poll stops the moment cmd is gone. detach-outside-job-log.
