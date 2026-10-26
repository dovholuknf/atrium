# r-superlean-default-for-items: item workers launch superlean

The hub's `atrium_launch` starts an isolated item worker superlean unless the caller says `superlean: false`. A review
stays lean, because superlean drops mercurius. A launch with `lean: false`, another runner or an interview stays as it
was, and so does every launch that is not an item worker. A caller's own `--tools` in `args` still wins.

`launchInput.Superlean` is a pointer so that absent and false differ. A room's stdio `atrium_launch` relayed to the hub
can ask for superlean and cannot ask for it off.
