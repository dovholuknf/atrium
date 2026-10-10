#!/usr/bin/env bash
# Build the atrium docs site (website/) into a static folder ready for GitHub Pages.
#
# All of the docs publishing logic lives here, so it runs the same on this machine as in
# .github/workflows/docs.yml. The workflow only checks out, calls this, and hands the output folder to the Pages
# actions. Nothing here publishes anything: it builds, checks, and stops.
#
# The workflow runs on a push to main that touches website/, docs/, the workflow or this script, and by hand.
#
# Checks, each fatal:
#   - npm ci, from the committed lockfile, so a build is the same build everywhere
#   - the starter gate script in docs/hooks.md, run against a mock of atrium's agent port
#   - docusaurus build, which already refuses broken links and anchors
#   - every root-relative href and src in the output starts with the base URL, so the site works under /atrium/
#   - .nojekyll is present, so Pages serves the assets folder Docusaurus writes
#
# Flags:
#   --url       The site's origin, e.g. https://dovholuknf.github.io (the default). The Pages action reports it
#               as `origin`.
#   --base-url  The path the site is served under, e.g. /atrium/ (the default). The Pages action reports it as
#               `base_path`, without the trailing slash, which this adds.
#   --out-dir   Where the finished site goes. Defaults to website/build.
#
# Example:
#   ./scripts/build-docs.sh
#   npx --prefix website docusaurus serve website --port 3031   # then open http://localhost:3031/atrium/

set -euo pipefail

url='https://dovholuknf.github.io'
base_url='/atrium/'
out_dir=''

while [ $# -gt 0 ]; do
  case "$1" in
    --url) url="${2?--url needs a value}"; shift 2 ;;
    --base-url) base_url="${2?--base-url needs a value}"; shift 2 ;;
    --out-dir) out_dir="${2?--out-dir needs a value}"; shift 2 ;;
    -h|--help) sed -n '2,25p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
site="$repo/website"
[ -n "$out_dir" ] || out_dir="$site/build"
# Resolve now, because the build runs from inside website/.
mkdir -p "$out_dir"
out_dir="$(cd "$out_dir" && pwd)"

case "$base_url" in /*) ;; *) base_url="/$base_url" ;; esac
case "$base_url" in */) ;; *) base_url="$base_url/" ;; esac
url="${url%/}"

step() {
  echo "==> $1"
}

cd "$site"
step 'npm ci'
npm ci --no-audit --no-fund
step 'the gate script in docs/hooks.md runs as documented'
node scripts/test-gate-hook.js docs/hooks.md

step "docusaurus build ($url$base_url)"
# git-bash rewrites a value like /atrium/ into a Windows path when it reaches a native program. Stop that.
export MSYS2_ENV_CONV_EXCL='ATRIUM_DOCS_URL;ATRIUM_DOCS_BASE_URL'
ATRIUM_DOCS_URL="$url" ATRIUM_DOCS_BASE_URL="$base_url" npx docusaurus build --out-dir "$out_dir"
cd "$repo"

echo "==> checking every link carries $base_url"
# Each match is `file:href="/..."`. Keep the ones whose target does not start with the base URL.
bad="$(grep -rEo --include='*.html' '(href|src)="(/[^"/][^"]*|/)"' "$out_dir" \
  | awk -v base="$base_url" '{
      i = index($0, "=\"")
      link = substr($0, i + 2, length($0) - i - 2)
      if (index(link, base) != 1) print
    }' || true)"
if [ -n "$bad" ]; then
  printf '%s\n' "$bad" | sed -n '1,20s/^/  /p'
  echo "links outside $base_url would break on Pages" >&2
  exit 1
fi

[ -e "$out_dir/.nojekyll" ] || : > "$out_dir/.nojekyll"

echo "built: $out_dir"
