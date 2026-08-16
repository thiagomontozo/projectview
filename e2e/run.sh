#!/usr/bin/env bash
# Browser-level tests against a running stack.
#
# Usage:
#   e2e/run.sh                    # against the compose stack
#   E2E_BASE_URL=... e2e/run.sh   # against something else
#
# Playwright runs inside its official image rather than on the host: the
# browsers it drives are a 400 MB download pinned to the library version, and
# the image already carries the matching pair. That also means this needs no
# node on the machine running it, which is the same reason the Go and frontend
# checks run in containers.
#
# The container joins the compose network and addresses the proxy by service
# name, so the run does not depend on how Docker publishes ports - which
# differs between Linux and Docker Desktop and is not what these tests are for.
set -uo pipefail

cd "$(dirname "$0")/.." || exit 1

# Git Bash on Windows rewrites anything that looks like a Unix path in an
# argument, so "-w /e2e" reaches Docker as "C:/Program Files/Git/e2e" and the
# run fails with an error about the working directory rather than about
# anything real. Switching the translation off is a no-op everywhere else.
export MSYS_NO_PATHCONV=1
export MSYS2_ARG_CONV_EXCL='*'

# The mount source has to stay a path the Docker daemon understands, which on
# Windows means the native form rather than the MSYS one.
host_path="$PWD"
case "$(uname -s)" in
MINGW* | MSYS* | CYGWIN*)
    host_path="$(pwd -W 2>/dev/null || echo "$PWD")"
    ;;
esac

# Must match @playwright/test in e2e/package.json. Mismatched pairs fail with
# "Executable doesn't exist", which reads as a broken install rather than a
# version skew.
IMAGE="${PLAYWRIGHT_IMAGE:-mcr.microsoft.com/playwright:v1.49.1-noble}"
NETWORK="${E2E_NETWORK:-projectview_default}"
BASE_URL="${E2E_BASE_URL:-https://proxy}"

if ! docker network inspect "$NETWORK" >/dev/null 2>&1; then
    echo "Network $NETWORK not found. Start the stack first: docker compose up -d" >&2
    exit 1
fi

echo "Running the browser suite against $BASE_URL"

# Optional extra root CA, the same one the image builds use. Node ships its own
# trust store, so on a network with a TLS-inspecting proxy npm fails to reach
# the registry with UNABLE_TO_VERIFY_LEAF_SIGNATURE unless it is told about the
# interceptor. Nothing is needed on an ordinary network; see ca/README.md.
ca_args=()
for cert in "$PWD"/ca/*.crt; do
    if [ -f "$cert" ]; then
        ca_args=(-v "$host_path/ca:/tmp/extra-ca:ro" -e NODE_EXTRA_CA_CERTS=/tmp/extra-ca/"$(basename "$cert")")
        break
    fi
done

docker run --rm \
    --network "$NETWORK" \
    --ipc=host \
    "${ca_args[@]}" \
    -e "E2E_BASE_URL=$BASE_URL" \
    -e "E2E_ADMIN_USER=${BOOTSTRAP_ADMIN_USERNAME:-admin}" \
    -e "E2E_ADMIN_PASS=${BOOTSTRAP_ADMIN_PASSWORD:-ChangeMe123!}" \
    -e "CI=${CI:-}" \
    -v "$host_path/e2e:/e2e" \
    -w /e2e \
    "$IMAGE" \
    sh -c 'npm ci --no-audit --no-fund >/dev/null 2>&1 || npm install --no-audit --no-fund; npx playwright test "$@"' -- "$@"

status=$?
if [ "$status" -ne 0 ]; then
    echo >&2
    echo "The browser suite failed. Traces and screenshots for the failing tests" >&2
    echo "are under e2e/test-results/; open the HTML report with:" >&2
    echo "  npx playwright show-report e2e/playwright-report" >&2
fi
exit "$status"
