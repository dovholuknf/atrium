#!/usr/bin/env sh
# Install atrium from its latest GitHub release and start it. Run it with:
#   curl -fsSL https://raw.githubusercontent.com/dovholuknf/atrium/main/install.sh | sh
# To pass options: ... | sh -s -- --dir /opt/atrium/bin --addr 127.0.0.1:7778
#   --dir D        install into D (default ~/.atrium/bin)
#   --version V    a release tag such as v0.0.5 (default: the latest)
#   --addr A       the board address (default 127.0.0.1:7778, or the next free ports when it is held)
#   --no-path      do not edit a shell profile
#   --no-start     install only
#   --             everything after goes to `atrium run`
# It downloads the archive for this OS and arch, checks it against the release's checksums.txt, puts atrium in the
# dir, adds the dir to your shell profile PATH, starts `atrium run` detached, and prints the board URL.
set -eu

dir="$HOME/.atrium/bin"; version=latest; repo="${ATRIUM_REPO:-dovholuknf/atrium}"; addr=
no_path=0; no_start=0
while [ $# -gt 0 ]; do
  case "$1" in
    --dir) dir="$2"; shift 2 ;;
    --version) version="$2"; shift 2 ;;
    --addr) addr="$2"; shift 2 ;;
    --no-path) no_path=1; shift ;;
    --no-start) no_start=1; shift ;;
    --) shift; break ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done

die() { echo "install: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }
need curl; need tar

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) die "no atrium release for $(uname -s), on Windows use install.ps1" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) die "no atrium release for architecture $(uname -m)" ;;
esac

if command -v sha256sum >/dev/null 2>&1; then sha() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then sha() { shasum -a 256 "$1" | cut -d' ' -f1; }
else die "sha256sum or shasum is required"; fi

api="https://api.github.com/repos/$repo/releases"
if [ "$version" = latest ]; then rel_url="$api/latest"; else rel_url="$api/tags/$version"; fi
tag="$(curl -fsSL "$rel_url" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
[ -n "$tag" ] || die "cannot read the release tag from $rel_url"
asset="atrium_${tag}_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$tag"
echo "installing atrium $tag ($asset)"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
curl -fsSL -o "$work/$asset" "$base/$asset" || die "cannot download $base/$asset"
curl -fsSL -o "$work/checksums.txt" "$base/checksums.txt" || die "cannot download $base/checksums.txt"

want="$(awk -v a="$asset" '{ n = $2; sub(/^\*/, "", n); if (n == a) print $1 }' "$work/checksums.txt" | head -n 1)"
[ -n "$want" ] || die "checksums.txt has no entry for $asset"
got="$(sha "$work/$asset")"
[ "$got" = "$want" ] || die "checksum mismatch for $asset: want $want, got $got"
echo "checksum ok"

mkdir -p "$work/x" "$dir"
tar -xzf "$work/$asset" -C "$work/x"
bin="$(find "$work/x" -type f -name atrium | head -n 1)"
[ -n "$bin" ] || die "$asset holds no atrium binary"
cp "$bin" "$dir/atrium.new" && chmod 755 "$dir/atrium.new" && mv -f "$dir/atrium.new" "$dir/atrium" \
  || die "cannot write $dir/atrium"
dir="$(cd "$dir" && pwd)"
echo "installed $dir/atrium"

if [ "$no_path" = 0 ]; then
  case ":$PATH:" in *":$dir:"*) ;; *)
    case "${SHELL:-}" in
      */zsh) rc="$HOME/.zshrc" ;;
      */bash) if [ "$os" = darwin ]; then rc="$HOME/.bash_profile"; else rc="$HOME/.bashrc"; fi ;;
      *) rc="$HOME/.profile" ;;
    esac
    if ! grep -qsF "$dir" "$rc"; then
      printf '\nexport PATH="%s:$PATH"\n' "$dir" >> "$rc"
      echo "added $dir to PATH in $rc (open a new terminal to use it)"
    fi ;;
  esac
fi

if [ "$no_start" = 1 ]; then echo "not started. start it with: atrium run"; exit 0; fi

# atrium run moves to free ports when the default ones are held, and writes the board it bound in hub-addr.json.
if [ -n "${WORKTREE_ROOT:-}" ]; then hub="$WORKTREE_ROOT/hub/hub"; else hub="$HOME/.atrium/hub"; fi
board() {
  b=$(sed -n 's/.*"board":"\([^"]*\)".*/\1/p' "$hub/hub-addr.json" 2>/dev/null)
  [ -n "$b" ] && curl -fsS -o /dev/null --max-time 2 "http://$b" 2>/dev/null && echo "http://$b"
}
if [ -n "$addr" ]; then set -- --addr "$addr" "$@"; fi
if command -v setsid >/dev/null 2>&1; then
  setsid nohup "$dir/atrium" run "$@" >"$dir/run.log" 2>&1 </dev/null &
else
  nohup "$dir/atrium" run "$@" >"$dir/run.log" 2>&1 </dev/null &
fi
pid=$!
i=0; url=
while [ $i -lt 60 ] && [ -z "$url" ]; do
  sleep 0.5; i=$((i + 1))
  if ! kill -0 "$pid" 2>/dev/null; then
    cat "$dir/run.log"
    wait "$pid" || die "atrium run stopped, see $dir/run.log"
    exit 0
  fi
  url=$(board) || url=
done
if [ -n "$url" ]; then echo "board: $url"; else echo "warning: atrium is still starting, its log is $dir/run.log" >&2; fi
