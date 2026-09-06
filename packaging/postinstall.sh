#!/bin/sh
# Enable atrium for the person who ran the install, and explain it when it cannot.
#
# THE SUBTLE PART, and the reason this file is mostly comment: the unit atrium
# ships is a USER unit, and this script runs as root. Those two facts fight each
# other, and every wrong way to resolve the fight looks like it worked.
#
# The wrong ways, named so nobody reaches for them again:
#
#   systemctl enable atrium            enables it as a SYSTEM unit. There is no
#                                      system unit by that name, so this either
#                                      fails or, worse on a machine that has
#                                      one, starts the wrong thing.
#   systemctl --user enable atrium     talks to ROOT's user manager, which is
#                                      not the manager anybody wanted, and on a
#                                      machine where root has no session it
#                                      fails with a message about a bus.
#   systemctl --global enable atrium   enables it for EVERY user on the machine,
#                                      now and in the future. That is a policy
#                                      decision about somebody's server, taken
#                                      by a package, which is not this package's
#                                      to take.
#
# What is correct is to find the one human who typed the install command, and to
# enable it for that human and nobody else.
#
# WHY THIS ENABLES AT ALL, since the earlier version of this file deliberately
# did not. "Installing says put this here, starting a daemon is a different
# sentence" is a good rule and it is wrong for atrium specifically. Atrium's
# whole job is to still be running when you come back to it: a card outliving
# the process it describes is the design, and an install that leaves you to
# start it by hand leaves you exactly where you were, which is a daemon somebody
# started once by hand months ago and could not restart without losing the
# board. Two escape hatches are provided instead, below.
#
# ATRIUM_NO_ENABLE=1   install the files and touch nothing else.
# ATRIUM_NO_LINGER=1   enable it, but do not survive logout.
#
# Arguments, because the two packaging systems disagree about them:
#   deb  postinst gets `configure <old-version>`
#   rpm  post     gets `1` for a first install and `2` for an upgrade
# Neither needs branching here: everything below is idempotent, and an upgrade
# wants the same end state as an install.

set -e

UNIT=atrium.service
UNIT_PATH=/usr/lib/systemd/user/atrium.service

say() { echo "atrium: $*"; }

if [ "${ATRIUM_NO_ENABLE:-}" = "1" ]; then
    say "ATRIUM_NO_ENABLE=1, so nothing was enabled."
    say "when you want it:  systemctl --user enable --now atrium"
    exit 0
fi

if ! command -v systemctl >/dev/null 2>&1; then
    say "no systemctl on this machine, so there is nothing to enable."
    say "run it yourself:  atrium daemon --db \$HOME/.atrium/atrium.db"
    exit 0
fi

# WHO INSTALLED THIS. In order of how much the answer can be trusted.
#
# SUDO_USER is set by sudo and is the account that ran it. PKEXEC_UID is the
# same idea from polkit, which is what a graphical package manager goes through.
# DOAS_USER covers the BSD-flavoured machines that use doas instead.
#
# A bare `root` answer is not a person. It means an unattended install: a
# container build, a cloud-init run, a configuration manager. Enabling anything
# for root there would produce a daemon nobody asked for, supervising sessions
# nobody is in.
target_user="${SUDO_USER:-}"
if [ -z "$target_user" ] && [ -n "${PKEXEC_UID:-}" ]; then
    target_user="$(getent passwd "$PKEXEC_UID" | cut -d: -f1)"
fi
if [ -z "$target_user" ]; then
    target_user="${DOAS_USER:-}"
fi

if [ -z "$target_user" ] || [ "$target_user" = "root" ]; then
    say "installed to /usr/bin/atrium."
    say "this looks like an unattended install, so nothing was enabled: there is"
    say "no logged-in person here whose session a runner could belong to."
    say ""
    say "as the person who will use it, not as root:"
    say "  systemctl --user enable --now atrium"
    say "  loginctl enable-linger \$USER    # to survive logging out"
    exit 0
fi

target_uid="$(id -u "$target_user" 2>/dev/null || true)"
target_home="$(getent passwd "$target_user" | cut -d: -f6)"
if [ -z "$target_uid" ] || [ -z "$target_home" ]; then
    say "cannot resolve the account '$target_user', so nothing was enabled."
    exit 0
fi

# THE DATABASE DIRECTORY, made here rather than by the daemon at start.
#
# The unit names the database explicitly (see atrium.service for why), and a
# unit whose ExecStart fails because a directory is missing fails at logon,
# where the only record is a journal nobody is reading.
if [ ! -d "$target_home/.atrium" ]; then
    mkdir -p "$target_home/.atrium"
    chown "$target_user" "$target_home/.atrium" 2>/dev/null || true
fi

# LINGERING, FIRST, and the order matters for a mechanical reason as well as a
# policy one.
#
# Policy: a user unit stops when you log out. Atrium outliving the session that
# started it is the entire point of v2, so lingering is on by default here,
# which reverses what this repository said when the unit was written and nothing
# had ever been installed. ATRIUM_NO_LINGER=1 keeps the old behaviour.
#
# Mechanics: `enable-linger` starts the user's systemd manager immediately if it
# is not already up, which creates /run/user/$uid. Everything below talks to
# that manager over that path, so doing this first turns "no session bus" from
# the common case into the rare one.
if [ "${ATRIUM_NO_LINGER:-}" = "1" ]; then
    say "ATRIUM_NO_LINGER=1: atrium will stop when $target_user logs out."
else
    loginctl enable-linger "$target_user" 2>/dev/null || \
        say "could not enable lingering for $target_user (continuing)."
fi

# TALKING TO SOMEBODY ELSE'S USER MANAGER.
#
# `systemctl --user` finds its manager through XDG_RUNTIME_DIR and the session
# bus underneath it. Root has neither of those for another account, so both are
# named explicitly and the command is dropped into that account with runuser.
#
# runuser rather than `su -`: it does not run a login shell, does not read that
# account's shell configuration, and cannot be surprised by a .bashrc that
# prints something or exits non-zero.
run_as_user() {
    runuser -u "$target_user" -- \
        env XDG_RUNTIME_DIR="/run/user/$target_uid" \
            DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$target_uid/bus" \
            "$@"
}

enabled=no
if [ -d "/run/user/$target_uid" ] && command -v runuser >/dev/null 2>&1; then
    if run_as_user systemctl --user daemon-reload 2>/dev/null; then
        if run_as_user systemctl --user enable --now "$UNIT" 2>/dev/null; then
            enabled=yes
        fi
    fi
fi

if [ "$enabled" = "no" ]; then
    # THE FALLBACK, and it is not a hack: writing this symlink is precisely
    # what `systemctl --user enable` does. The unit's [Install] section says
    # WantedBy=default.target, and enabling it means a link from that target's
    # .wants directory to the unit file. Doing it by hand works with no running
    # manager, no bus, and no runuser, which is the case on a machine where the
    # person who ran the install has never logged in graphically.
    #
    # What is lost is the `--now`: it will start at their next login rather
    # than this second. That is said out loud below rather than hidden.
    wants="$target_home/.config/systemd/user/default.target.wants"
    mkdir -p "$wants"
    ln -sf "$UNIT_PATH" "$wants/$UNIT"
    chown -R "$target_user" "$target_home/.config/systemd" 2>/dev/null || true
    say "enabled atrium for $target_user at their next login."
    say "there was no running user manager to start it in right now."
    say "to start it without waiting:  systemctl --user start atrium"
    exit 0
fi

say "atrium is enabled and running for $target_user."
say "  systemctl --user status atrium"
say "  journalctl --user -u atrium -f"
say "  the board:  http://localhost:7778"
exit 0
