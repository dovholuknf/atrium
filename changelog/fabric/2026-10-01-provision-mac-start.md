`provision-room.ps1` on a Mac with nobody logged in at the desktop no longer leaves the room down. The LaunchAgent only
loads at a desktop login, so when it is not loaded and the room does not answer, provision starts it detached and says
that a reboot with nobody logged in leaves it down until auto-login is on. m1mini went down at its 10:46 reboot for this
reason: the room had been started by hand with `--detach` and the LaunchAgent never loaded.
