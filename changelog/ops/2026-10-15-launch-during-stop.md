A launch that arrives while the room is stopping or restarting is now refused with a retryable error, where it used to
answer `running` for a runner the restart took before its opening prompt landed (sgg, deploy with `-Rooms all`). Item
r-launch-prompt-lost-in-deploy.
