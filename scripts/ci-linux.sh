#!/usr/bin/env bash
# scripts/ci.sh on Linux, in docker, from any machine that has docker.
#
#   scripts/ci-linux.sh            run it once
#   scripts/ci-linux.sh 3          run it three times, stopping at the first failure
#   scripts/ci-linux.sh -- go test -count=20 -run TestX ./internal/daemon/
#                                  anything else, in the same container, once
#
# THE UBUNTU JOB, AS CLOSE AS A CONTAINER GETS. The same Go as go.mod, node 22,
# a user that is not root (the runner is uid 1001, and a test of a permission
# refusal passes as root), four CPUs like the runner, no pwsh (nothing on Linux
# runs PowerShell, so a step that wants it is the bug), and
# a fresh copy of the tree rather than a mount of it: what a checkout would
# hold, plus whatever is uncommitted here, and nothing .gitignore leaves out. A
# mount would hand the container this machine's build.claude/ and a .git file
# that points at a Windows path.
#
# The artefacts ci.sh writes (build.claude/ci/) come back to
# build.claude/ci-linux/<when>-<run>/ here, so a failure in the container is
# read the same way as a failure on GitHub.
#
# NO `docker run -i`. The tree goes in with `docker cp`, the output comes out
# with `docker logs -f` and the exit code with `docker wait`. None of those
# attach to the container, and attaching is what a docker daemon reached over
# tcp (sg4's, at DOCKER_HOST=tcp://127.0.0.1:2375) silently drops.

set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$here"

runs="${1:-1}"
cmd="bash scripts/ci.sh"
if [ "$runs" = "--" ]; then
  shift
  runs=1
  cmd="$(printf '%q ' "$@")"
fi
gover="$(sed -n 's/^go \([0-9.]*\)$/\1/p' go.mod)"
if [ -z "$gover" ]; then
  echo "no go version in go.mod" >&2
  exit 2
fi
image="atrium-ci-linux:go$gover"

# Built once per Go version and reused. Nothing in it depends on the tree.
if ! docker image inspect "$image" >/dev/null 2>&1; then
  echo "=== building $image"
  docker build -t "$image" --build-arg GO="$gover" - <<'EOF' || exit 2
ARG GO
FROM golang:${GO}
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl git xz-utils less \
 && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL https://nodejs.org/dist/v22.20.0/node-v22.20.0-linux-x64.tar.xz | tar -xJ -C /usr/local --strip-components=1
RUN useradd -m -u 1001 runner \
 && mkdir -p /src /home/runner/work /home/runner/go/pkg/mod /home/runner/.cache/go-build \
 && chown -R runner:runner /src /home/runner
USER runner
ENV GOPATH=/home/runner/go GOFLAGS=-buildvcs=false
WORKDIR /home/runner/work
EOF
fi

i=1
while [ "$i" -le "$runs" ]; do
  name="atrium-ci-linux-$$-$i"
  out="$here/build.claude/ci-linux/$(date +%Y%m%d-%H%M%S)-$i"
  mkdir -p "$out"
  echo "=== linux run $i of $runs: container $name, artefacts in $out"
  docker create --name "$name" --cpus 4 -e CI=true \
    -v atrium-ci-gomod:/home/runner/go/pkg/mod \
    -v atrium-ci-gocache:/home/runner/.cache/go-build \
    "$image" bash -c '
      set -e
      cp -r /src/. .
      git init -q
      git add -A
      git -c user.email=ci@localhost -c user.name=ci commit -qm ci
      set +e
      '"$cmd" >/dev/null || exit 2
  # What a checkout holds plus what is uncommitted. --ignore-failed-read
  # skips a tracked file that has been deleted here.
  git ls-files -z -co --exclude-standard \
    | tar --null --ignore-failed-read -T - -cf - \
    | docker cp - "$name:/src" || { docker rm "$name" >/dev/null; exit 2; }
  docker start "$name" >/dev/null || exit 2
  docker logs -f "$name"
  rc="$(docker wait "$name")"
  docker cp "$name:/home/runner/work/build.claude/ci/." "$out/" >/dev/null 2>&1 || true
  docker rm "$name" >/dev/null
  if [ "$rc" != "0" ]; then
    echo "linux run $i of $runs FAILED (exit $rc). artefacts: $out" >&2
    exit 1
  fi
  echo "linux run $i of $runs passed"
  i=$((i + 1))
done
