#!/bin/sh
# Stop and disable atrium before the files go away, and not on an upgrade.
#
# THE ARGUMENT CHECK IS THE WHOLE POINT OF THIS FILE. Both packaging systems run
# this script on an upgrade as well as on a removal, and they say which with
# different words:
#
#   deb  prerm gets `remove`, or `upgrade <version>` when a newer one is coming
#   rpm  preun gets `0` for the last copy going away, `1` when one is replacing
#        another
#
# Disabling on an upgrade means `apt upgrade atrium` quietly turns atrium off,
# and the person who ran it finds out at their next reboot rather than now. So
# an upgrade returns immediately and the postinstall puts the new binary under
# a unit that is still enabled and still running.

set -e

case "${1:-}" in
    upgrade|failed-upgrade|deconfigure|1)
        exit 0
        ;;
esac

say() { echo "atrium: $*"; }

command -v systemctl >/dev/null 2>&1 || exit 0

# WHICH ACCOUNTS TO CLEAN UP, and why the answer is a list rather than a name.
#
# The postinstall enabled it for exactly one person, but that was one install
# ago and there may have been several. The two places that record a user having
# atrium enabled are the linger directory, which the postinstall writes to, and
# the symlink in the account's own .wants directory. Both are checked, and the
# account that ran this command is added because it is the most likely answer
# and may be neither.
#
# Best effort throughout: a removal that fails because one stale home directory
# is unreadable is worse than a removal that leaves a dangling symlink behind.
users=""
if [ -d /var/lib/systemd/linger ]; then
    for f in /var/lib/systemd/linger/*; do
        [ -e "$f" ] || continue
        users="$users $(basename "$f")"
    done
fi
[ -n "${SUDO_USER:-}" ] && users="$users $SUDO_USER"

for u in $users; do
    [ "$u" = "root" ] && continue
    uid="$(id -u "$u" 2>/dev/null || true)"
    home="$(getent passwd "$u" | cut -d: -f6)"
    [ -n "$uid" ] || continue
    [ -n "$home" ] || continue

    link="$home/.config/systemd/user/default.target.wants/atrium.service"

    if [ -d "/run/user/$uid" ] && command -v runuser >/dev/null 2>&1; then
        # `disable --now` is one call that stops it and removes the symlink.
        # Stopping is a SIGTERM, which is the daemon's own wind-down: it
        # releases the event streams, gives every supervised runner ten seconds
        # and closes the listeners. KillMode=mixed in the unit is what keeps
        # systemd from killing the whole cgroup out from under that.
        runuser -u "$u" -- \
            env XDG_RUNTIME_DIR="/run/user/$uid" \
                DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$uid/bus" \
                systemctl --user disable --now atrium.service 2>/dev/null || true
    fi

    # Whether or not the manager answered, the symlink is ours to remove. A
    # dangling one makes every later `systemctl --user` call print a warning
    # about a unit file that is not there.
    [ -L "$link" ] && rm -f "$link"
done

say "stopped and disabled where it was enabled. lingering was left alone:"
say "it may have been on before atrium, and turning it off is not ours to do."
exit 0
