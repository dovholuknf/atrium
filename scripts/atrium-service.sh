#!/usr/bin/env bash
# Install, remove, start, stop and inspect atrium as a managed service.
#
#   scripts/atrium-service.sh install
#   scripts/atrium-service.sh status
#   scripts/atrium-service.sh stop
#   scripts/atrium-service.sh start
#   scripts/atrium-service.sh restart
#   scripts/atrium-service.sh uninstall
#
# ONE SCRIPT FOR TWO INIT SYSTEMS, because there is one decision here and it has
# two spellings. On Linux it is a systemd USER unit; on macOS it is a launchd
# LaunchAGENT. Both run as you, in your session, which is the whole point: a
# system service or a LaunchDaemon has none of your PATH, shell configuration,
# ssh agent, keychain or Claude Code configuration, and every runner it started
# would inherit none of them. Supervision is most of what atrium is for, so a
# system-context atrium is atrium with its main feature missing.
#
# The Windows half of the same decision is `scripts/atrium-service.ps1`, which
# registers a logon task for the same reason. Change them together.
#
# EVERY VERB IS IDEMPOTENT. Installing twice re-registers over the top rather
# than producing two of anything, which is the failure mode that matters: two
# daemons on one database. Uninstalling twice says so and succeeds. Starting
# something already started, and stopping something already stopped, are both
# fine and both say what they found.
#
# This is also what the CI workflow would call, if there were a Linux machine
# here to call it on. There is not, and docs/packaging.md says which parts of
# this file have therefore never executed.

set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

LABEL="io.github.dovholuknf.atrium"
UNIT="atrium.service"

# --- what we are working with -----------------------------------------------

# ATRIUM_EXE, ATRIUM_DB and ATRIUM_SERVICE_ARGS are the three things somebody
# might reasonably need to move, and all three are read from the environment
# rather than parsed as flags. A flag would have to be repeated on every verb,
# and the verbs that need them are the ones you type once.
db="${ATRIUM_DB:-$HOME/.atrium/atrium.db}"
extra_args="${ATRIUM_SERVICE_ARGS:-}"

find_exe() {
    if [ -n "${ATRIUM_EXE:-}" ]; then
        printf '%s' "$ATRIUM_EXE"
        return
    fi
    # PATH FIRST, and a package manager's copy is what lands there.
    #
    # NOT the build in this checkout. A path under build.claude/ is a moving
    # identity: it changes with the checkout, every `go build` rewrites it, and
    # a service registration pointing there survives exactly until the repo
    # moves. This is the same defect `scripts/atrium-autostart.ps1` records
    # having had.
    if command -v atrium >/dev/null 2>&1; then
        command -v atrium
        return
    fi
    printf ''
}

exe="$(find_exe)"

require_exe() {
    if [ -z "$exe" ] || [ ! -x "$exe" ]; then
        cat >&2 <<EOF
no atrium on PATH, and ATRIUM_EXE is not set.

install one, or say where it is:
  ATRIUM_EXE=/usr/bin/atrium scripts/atrium-service.sh install

a build in this checkout is deliberately not the default: a service pointing at
build.claude/ breaks the moment the repository moves or is rebuilt.
EOF
        exit 1
    fi
}

case "$(uname -s)" in
    Darwin) platform=macos ;;
    Linux)  platform=linux ;;
    *)      echo "this script covers Linux and macOS. on Windows use scripts/atrium-service.ps1." >&2
            exit 1 ;;
esac

say() { echo "atrium: $*"; }

# --- linux, systemd user unit ------------------------------------------------

linux_unit_dir="$HOME/.config/systemd/user"

linux_install() {
    require_exe
    mkdir -p "$linux_unit_dir" "$(dirname "$db")"

    # WHERE THE UNIT COMES FROM depends on how atrium got here, and both answers
    # are correct.
    #
    # A packaged atrium already put the vendor unit in /usr/lib/systemd/user and
    # its postinstall already enabled it. Running this afterwards should not
    # copy a second one into the home directory, because then an upgrade of the
    # package updates a file systemd is no longer reading.
    #
    # A hand-installed atrium has no vendor unit, so one is written into the
    # user directory, with ExecStart pointed at the binary we actually found.
    if [ -f /usr/lib/systemd/user/$UNIT ] || [ -f /lib/systemd/user/$UNIT ]; then
        say "using the packaged unit in /usr/lib/systemd/user."
    else
        # %h is systemd's own expansion for the user's home, so the written
        # unit stays correct if the home directory is ever a different path
        # inside a container or on a network mount.
        sed -e "s#^ExecStart=.*#ExecStart=$exe daemon --db $db $extra_args#" \
            "$here/packaging/atrium.service" > "$linux_unit_dir/$UNIT"
        say "wrote $linux_unit_dir/$UNIT"
    fi

    systemctl --user daemon-reload

    # LINGER FIRST, for the same reason the package postinstall does it first:
    # it starts the user manager if it is not up, and everything after this
    # needs that manager. ATRIUM_NO_LINGER=1 opts out, and then atrium stops
    # when you log out.
    if [ "${ATRIUM_NO_LINGER:-}" = "1" ]; then
        say "ATRIUM_NO_LINGER=1: atrium will stop when you log out."
    else
        loginctl enable-linger "$USER" 2>/dev/null || \
            say "could not enable lingering (continuing). atrium will stop at logout."
    fi

    # `enable --now` is idempotent in both halves: enabling an enabled unit
    # rewrites the same symlink, and starting a started one is a no-op.
    systemctl --user enable --now "$UNIT"
    say "enabled and running."
    linux_status
}

linux_uninstall() {
    if systemctl --user list-unit-files "$UNIT" >/dev/null 2>&1; then
        # `disable --now` stops it with SIGTERM, which is the daemon's own
        # wind-down rather than a kill. A kill takes every supervised runner
        # with it, which is the distinction `atrium stop` exists to make.
        systemctl --user disable --now "$UNIT" 2>/dev/null || true
    fi
    if [ -f "$linux_unit_dir/$UNIT" ]; then
        rm -f "$linux_unit_dir/$UNIT"
        say "removed $linux_unit_dir/$UNIT"
    else
        say "no unit of ours in $linux_unit_dir. nothing to remove there."
    fi
    systemctl --user daemon-reload 2>/dev/null || true

    # LINGERING IS LEFT ON, deliberately. It may have been on before atrium
    # existed on this machine, and turning off something we did not necessarily
    # turn on is how an uninstall breaks something unrelated.
    say "lingering was left as it is. turn it off yourself if atrium set it:"
    say "  loginctl disable-linger $USER"
}

linux_status() {
    systemctl --user status "$UNIT" --no-pager --lines=0 || true
}

# --- macos, launchd LaunchAgent ----------------------------------------------

macos_plist="$HOME/Library/LaunchAgents/$LABEL.plist"
macos_logdir="$HOME/Library/Logs/atrium"
macos_target="gui/$(id -u)/$LABEL"

macos_install() {
    require_exe
    mkdir -p "$HOME/Library/LaunchAgents" "$macos_logdir" "$(dirname "$db")"

    # THE TEMPLATE IS RENDERED, because a plist cannot expand anything. launchd
    # reads it as data, so every path has to be absolute and already right.
    # There is no macOS equivalent of systemd's %h.
    sed -e "s#@SHELL@#${SHELL:-/bin/zsh}#g" \
        -e "s#@EXE@#$exe#g" \
        -e "s#@DB@#$db#g" \
        -e "s#@LOGDIR@#$macos_logdir#g" \
        -e "s#@HOME@#$HOME#g" \
        "$here/packaging/atrium.plist" > "$macos_plist"
    say "wrote $macos_plist"

    # IDEMPOTENCE ON launchd IS AN EXPLICIT BOOTOUT, not a flag.
    #
    # `bootstrap` on a label that is already loaded fails with "service already
    # loaded" (error 5) and changes nothing, so re-running the install after
    # editing the plist would silently keep the old one. Booting it out first
    # is the supported way to reload, and it is allowed to fail because on a
    # first install there is nothing to boot out.
    launchctl bootout "$macos_target" 2>/dev/null || true
    launchctl bootstrap "gui/$(id -u)" "$macos_plist"
    launchctl enable "$macos_target"
    say "loaded and running."
    macos_status
}

macos_uninstall() {
    if [ -f "$macos_plist" ]; then
        launchctl bootout "$macos_target" 2>/dev/null || true
        rm -f "$macos_plist"
        say "removed $macos_plist"
    else
        say "no LaunchAgent at $macos_plist. nothing to remove."
    fi
}

macos_status() {
    if [ ! -f "$macos_plist" ]; then
        say "not installed: no $macos_plist"
        return 0
    fi
    # `print` is verbose on purpose here: the state, the last exit status and
    # the pid are the three things worth seeing, and cutting it down to one of
    # them is how a status command stops being useful.
    launchctl print "$macos_target" 2>/dev/null | \
        grep -E '^\s*(state|pid|last exit code|path) ' || \
        say "loaded but launchctl would not describe it."
    say "log: $macos_logdir/atrium.log"
}

# --- the graceful stop, which is the same on both -----------------------------

# `atrium stop` FIRST, and the init system second.
#
# A kill is not a stop. The daemon owns a pseudo terminal per supervised runner
# and closing one takes the attached process with it, so the wind-down has to
# come from atrium itself: it releases the event streams, gives the runners ten
# seconds and closes the listeners. systemd and launchd both send SIGTERM, which
# the daemon also handles, but only `atrium stop` is guaranteed to have said so
# over the API and returned when it is actually done.
graceful_stop() {
    if [ -n "$exe" ] && [ -x "$exe" ]; then
        "$exe" stop 2>/dev/null || true
    fi
}

# --- verbs -------------------------------------------------------------------

verb="${1:-status}"

case "$verb" in
    install)
        # `if`, not `a && b || c`. That idiom runs the macOS branch when the
        # Linux one merely fails, which would be a very confusing way to learn
        # that systemctl was missing.
        if [ "$platform" = linux ]; then linux_install; else macos_install; fi
        ;;
    uninstall|remove)
        graceful_stop
        if [ "$platform" = linux ]; then linux_uninstall; else macos_uninstall; fi
        ;;
    start)
        if [ "$platform" = linux ]; then
            systemctl --user start "$UNIT"
        else
            launchctl kickstart "$macos_target"
        fi
        say "started."
        ;;
    stop)
        graceful_stop
        if [ "$platform" = linux ]; then
            systemctl --user stop "$UNIT" 2>/dev/null || true
        else
            # `kill -TERM` rather than `stop`: with KeepAlive set to restart on
            # a bad exit, `launchctl stop` and a clean exit are the same thing
            # and neither loops. TERM is what the daemon listens for.
            launchctl kill TERM "$macos_target" 2>/dev/null || true
        fi
        say "stopped."
        ;;
    restart)
        graceful_stop
        if [ "$platform" = linux ]; then
            systemctl --user restart "$UNIT"
        else
            launchctl kickstart -k "$macos_target"
        fi
        say "restarted."
        ;;
    status)
        if [ "$platform" = linux ]; then linux_status; else macos_status; fi
        ;;
    *)
        echo "usage: $0 {install|uninstall|start|stop|restart|status}" >&2
        exit 2
        ;;
esac
