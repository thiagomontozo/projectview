#!/usr/bin/env bash
# M3 load test: seeds a 10,000-task project, then measures the read paths.
#
# Usage:
#   scripts/loadtest/run.sh                 # both profiles
#   scripts/loadtest/run.sh unbounded       # only what ships today
#   scripts/loadtest/run.sh paginated       # only the proposed shape
#   scripts/loadtest/run.sh --clean         # remove the fixture and stop
#
# Requires the stack to be up (docker compose up -d).
#
# k6 runs inside the compose network and addresses the proxy by service name
# rather than through the published port. That keeps the run independent of how
# Docker publishes ports on the host, which differs between Linux and Docker
# Desktop and is not what this test is about.
set -uo pipefail

cd "$(dirname "$0")/../.." || exit 1

PROFILE="${1:-both}"
NETWORK="${LOADTEST_NETWORK:-projectview_default}"
K6_IMAGE="${K6_IMAGE:-grafana/k6}"

psql_fixture() {
    docker compose exec -T postgres \
        psql -U "${POSTGRES_USER:-projectview}" -d "${POSTGRES_DB:-projectview}" \
        -v ON_ERROR_STOP=1 "$@"
}

if [ "$PROFILE" = "--clean" ]; then
    echo "Removing the load-test fixture..."
    # One DELETE: the project cascades to its tasks, and everything hanging off
    # them. The generated accounts go separately - they are not owned by it.
    psql_fixture -c "DELETE FROM projects WHERE id = '11111111-1111-4111-8111-111111111111';" \
                 -c "DELETE FROM users WHERE username LIKE 'loadtest_user_%';"
    exit $?
fi

if ! docker compose ps --format '{{.Service}}' | grep -q postgres; then
    echo "The stack is not running. Start it with: docker compose up -d" >&2
    exit 1
fi

echo "=== Seeding the fixture (idempotent) ==="
if ! psql_fixture -f - < scripts/loadtest/seed.sql; then
    echo "seeding failed" >&2
    exit 1
fi

run_profile() {
    local profile="$1"
    echo
    echo "=== k6: PROFILE=$profile ==="
    # The exit code is deliberately not propagated: a crossed threshold means
    # the milestone is unmet, which is a result to record, not a broken run.
    # The caller reads the numbers.
    docker run --rm --network "$NETWORK" \
        -e "PROFILE=$profile" \
        -e "THINK_SECONDS=${THINK_SECONDS:-1}" \
        -e "VUS=${VUS:-10}" \
        -v "$PWD/scripts/loadtest:/scripts" \
        -i "$K6_IMAGE" run --quiet /scripts/m3.js
    echo "(k6 exit: $? - a non-zero code here means a threshold was crossed)"
}

case "$PROFILE" in
both)
    run_profile unbounded
    run_profile paginated
    ;;
unbounded | paginated)
    run_profile "$PROFILE"
    ;;
*)
    echo "unknown profile: $PROFILE (expected unbounded, paginated, both or --clean)" >&2
    exit 1
    ;;
esac

echo
echo "The fixture is left in place so a run can be repeated without reseeding."
echo "Remove it with: scripts/loadtest/run.sh --clean"
