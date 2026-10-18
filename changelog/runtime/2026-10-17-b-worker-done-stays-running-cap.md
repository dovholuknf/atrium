- A scratch worker is no longer tagged `atrium:keep-open` by default, so its final `atrium_done` exits its runner a few
  seconds later like any other worker and `atrium_cull` can reclaim its directory. It no longer holds a launch slot until
  somebody exits it. The card and directory stay and a `wake: true` say resumes it. A keep-open or investigation tag
  passed on purpose, `resident: true` and a blocked worker are unchanged. b-worker-done-stays-running-cap.
