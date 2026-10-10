A second graceful `atrium stop` during a wrap-up now joins the running wait and reports its outcome, instead of
stopping at once. The restart-wrap `?wait=` is capped at the setting's 3600 s maximum, and the room-restart client
times out at the configured wait plus a minute, not an hour (r-graceful-restart-second-stop).
