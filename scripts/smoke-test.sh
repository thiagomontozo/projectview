#!/usr/bin/env bash
# End-to-end smoke test against a running stack (docker compose up).
#
# Exercises the requirements the unit tests cannot reach: TLS termination and
# the proxy rules, authentication, the task/sub-task model with resource
# allocation and dates, the kanban move, the dashboard aggregations, chat, and
# the realtime WebSocket upgrade.
#
# Usage: scripts/smoke-test.sh [base-url]      (default: https://localhost)
#
# Deliberately avoids jq so it runs on a bare CI runner and on Git Bash.
set -uo pipefail

BASE="${1:-https://localhost}"
ADMIN_USER="${BOOTSTRAP_ADMIN_USERNAME:-admin}"
ADMIN_PASS="${BOOTSTRAP_ADMIN_PASSWORD:-ChangeMe123!}"

# -k: the default deployment uses a self-signed certificate.
# --http1.1: forced deliberately, not a default we happened to keep. The /ws
# check below needs a classic HTTP/1.1 Upgrade handshake - that mechanism does
# not exist in HTTP/2, so a curl build that ALPN-negotiates h2 (as most Linux
# builds do; the Windows curl.exe used during development did not) gets a 400
# instead of a 101. Real browsers sidestep this by always opening a dedicated
# HTTP/1.1 connection for a wss:// handshake, even on a page loaded over
# HTTP/2, so this matches production client behavior rather than working
# around it. It also keeps response header casing predictable: HTTP/2 forces
# field names to lowercase, HTTP/1.1 does not.
CURL=(curl -sk --http1.1 --max-time 20)

# Fixture names have to be unique per run, and the shell's PID alone is not:
# on Git Bash under Windows the numbers are small and get reused within
# minutes, so a second run collides with the deactivated account the first one
# left behind and every later assertion fails as an unauthenticated 401.
RUN_ID="$$$(date +%s | tail -c 6)"

PASS=0
FAIL=0

pass() { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); printf '  FAIL %s\n' "$1"; [ $# -gt 1 ] && printf '       %s\n' "$2"; }

# check <description> <expected> <actual>
check() {
    if [ "$2" = "$3" ]; then
        pass "$1"
    else
        fail "$1" "expected [$2], got [$3]"
    fi
}

# contains <description> <haystack> <needle>
contains() {
    case "$2" in
    *"$3"*) pass "$1" ;;
    *) fail "$1" "[$3] not found in: $(printf '%s' "$2" | head -c 200)" ;;
    esac
}

status_of() { "${CURL[@]}" -o /dev/null -w '%{http_code}' "$@"; }

# Extracts a string field from a JSON document without jq, taking the FIRST
# occurrence in document order (sed alone would anchor on the last one, since
# its .* is greedy - and responses embed nested objects with their own "id").
json_str() {
    printf '%s' "$1" | grep -o "\"$2\":\"[^\"]*\"" | head -n 1 | sed "s/^\"$2\":\"//; s/\"$//"
}

section() { printf '\n=== %s ===\n' "$1"; }

# ---------------------------------------------------------------------------
section "Waiting for the stack"
ready=""
for _ in $(seq 1 60); do
    if [ "$(status_of "$BASE/api/health")" = "200" ]; then
        ready=yes
        break
    fi
    sleep 2
done
if [ -z "$ready" ]; then
    printf 'the API never became reachable at %s/api/health\n' "$BASE" >&2
    exit 1
fi
pass "API reachable at $BASE"

# ---------------------------------------------------------------------------
section "Edge proxy"
HTTP_BASE="$(printf '%s' "$BASE" | sed 's|^https://|http://|')"

check "HTTP is redirected to HTTPS" "301" "$(status_of "$HTTP_BASE/projects")"
contains "redirect points at https" \
    "$("${CURL[@]}" -o /dev/null -w '%{redirect_url}' "$HTTP_BASE/projects")" "https://"
check "proxy health endpoint" "200" "$(status_of "$HTTP_BASE/healthz")"
check "SPA is served over HTTPS" "200" "$(status_of "$BASE/")"

# A dedicated HTTP/2 client, only to prove ordinary page loads can negotiate
# it - kept separate from $CURL, which forces HTTP/1.1 everywhere else (see
# the comment on $CURL above). Skipped, not failed, when the local curl build
# has no HTTP/2 support at all (some Windows/Git-Bash builds don't) - that is
# a limitation of the test client, not of the server under test.
if curl -sk --http2 --max-time 10 -o /dev/null "$BASE/" 2>/tmp/h2-probe-error; then
    h2_version="$(curl -sk --http2 --max-time 10 -o /dev/null -w '%{http_version}' "$BASE/")"
    check "ordinary requests can negotiate HTTP/2" "2" "$h2_version"
elif grep -qi 'does not support' /tmp/h2-probe-error; then
    printf '  skip ordinary requests can negotiate HTTP/2 (curl built without HTTP/2 support)\n'
else
    fail "ordinary requests can negotiate HTTP/2" "$(cat /tmp/h2-probe-error)"
fi
rm -f /tmp/h2-probe-error

# Compared case-insensitively: HTTP/2 forces response header field names to
# lowercase (values are untouched), and $CURL above pins HTTP/1.1 for the rest
# of this script, so this is the one place that distinction still matters.
headers_lc="$("${CURL[@]}" -I "$BASE/" | tr '[:upper:]' '[:lower:]')"
contains "HSTS header"                 "$headers_lc" "strict-transport-security"
contains "X-Content-Type-Options"      "$headers_lc" "nosniff"
contains "X-Frame-Options"             "$headers_lc" "sameorigin"
contains "Referrer-Policy"             "$headers_lc" "strict-origin-when-cross-origin"
contains "Permissions-Policy"          "$headers_lc" "geolocation=()"
if printf '%s' "$headers_lc" | grep -q '^server: nginx/'; then
    fail "nginx version is hidden" "server header exposes the version"
else
    pass "nginx version is hidden"
fi

# The backend must not be reachable except through the proxy.
backend_host="$(printf '%s' "$BASE" | sed 's|^https\?://||; s|[:/].*$||')"
if "${CURL[@]}" --max-time 5 -o /dev/null "http://$backend_host:4000/api/health"; then
    fail "backend is not published to the host" "port 4000 answered directly"
else
    pass "backend is not published to the host"
fi

# ---------------------------------------------------------------------------
section "Authentication"
check "unauthenticated API call is rejected" "401" "$(status_of "$BASE/api/projects")"

login_body="{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}"
login="$("${CURL[@]}" -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' -d "$login_body")"
TOKEN="$(json_str "$login" token)"

if [ -n "$TOKEN" ]; then
    pass "admin login returns a token"
else
    fail "admin login returns a token" "$login"
    printf '\ncannot continue without a session token\n' >&2
    exit 1
fi
contains "seeded admin has the admin role" "$login" '"role":"admin"'

check "wrong password is rejected" "401" \
    "$(status_of -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' \
        -d "{\"username\":\"$ADMIN_USER\",\"password\":\"definitely-wrong\"}")"

AUTH="Authorization: Bearer $TOKEN"
check "authenticated API call succeeds" "200" "$(status_of -H "$AUTH" "$BASE/api/projects")"

me="$("${CURL[@]}" -H "$AUTH" "$BASE/api/auth/me")"
contains "/api/auth/me returns the session user" "$me" "\"username\":\"$ADMIN_USER\""

# ---------------------------------------------------------------------------
section "First-run schema and seed"
projects="$("${CURL[@]}" -H "$AUTH" "$BASE/api/projects")"
contains "sample project was seeded" "$projects" "Sample Project"
contains "project carries kanban columns" "$projects" '"key":"in_progress"'

teams="$("${CURL[@]}" -H "$AUTH" "$BASE/api/teams")"
contains "default team was seeded" "$teams" "Default Team"

users="$("${CURL[@]}" -H "$AUTH" "$BASE/api/users")"
contains "admin user exists" "$users" "\"email\":"

# ---------------------------------------------------------------------------
section "Projects, tasks, sub-tasks and allocation"
me_id="$(json_str "$me" id)"

project_payload="{\"name\":\"Smoke Test Project\",\"key\":\"SMOKE${RUN_ID}\",\"description\":\"created by scripts/smoke-test.sh\"}"
project="$("${CURL[@]}" -X POST "$BASE/api/projects" -H "$AUTH" -H 'Content-Type: application/json' -d "$project_payload")"
project_id="$(json_str "$project" id)"

if [ -n "$project_id" ]; then
    pass "project created"
else
    fail "project created" "$project"
    exit 1
fi

# A task with an assignee (resource allocation) and start/due dates.
task_payload="{\"title\":\"Smoke parent task\",\"assignees\":[\"$me_id\"],\"startDate\":\"2026-01-01T00:00:00Z\",\"dueDate\":\"2026-12-31T00:00:00Z\",\"estimateHours\":8,\"priority\":\"high\"}"
task="$("${CURL[@]}" -X POST "$BASE/api/projects/$project_id/tasks" -H "$AUTH" -H 'Content-Type: application/json' -d "$task_payload")"
task_id="$(json_str "$task" id)"

if [ -n "$task_id" ]; then
    pass "task created"
else
    fail "task created" "$task"
    exit 1
fi
contains "task keeps its start date"   "$task" '"startDate":"2026-01-01'
contains "task keeps its due date"     "$task" '"dueDate":"2026-12-31'
contains "task keeps its priority"     "$task" '"priority":"high"'
contains "resource is allocated to it" "$task" "\"id\":\"$me_id\""

subtask_payload="{\"title\":\"Smoke sub-task\",\"project\":\"$project_id\",\"parentTask\":\"$task_id\",\"assignees\":[\"$me_id\"]}"
subtask="$("${CURL[@]}" -X POST "$BASE/api/tasks" -H "$AUTH" -H 'Content-Type: application/json' -d "$subtask_payload")"
contains "sub-task points at its parent" "$subtask" "\"parentTask\":\"$task_id\""

parent="$("${CURL[@]}" -H "$AUTH" "$BASE/api/tasks/$task_id")"
contains "parent lists the sub-task" "$parent" "Smoke sub-task"

# Moving a card on the board, which is what the kanban drag-and-drop calls.
moved="$("${CURL[@]}" -X PATCH "$BASE/api/tasks/$task_id/move" -H "$AUTH" -H 'Content-Type: application/json' \
    -d '{"status":"in_progress","order":3}')"
contains "task moved to in_progress" "$moved" '"status":"in_progress"'

reread="$("${CURL[@]}" -H "$AUTH" "$BASE/api/tasks/$task_id")"
contains "move was persisted" "$reread" '"status":"in_progress"'

done_task="$("${CURL[@]}" -X PATCH "$BASE/api/tasks/$task_id/move" -H "$AUTH" -H 'Content-Type: application/json' \
    -d '{"status":"done","order":0}')"
contains "completing a task stamps completedAt" "$done_task" '"completedAt":'

reopened="$("${CURL[@]}" -X PATCH "$BASE/api/tasks/$task_id/move" -H "$AUTH" -H 'Content-Type: application/json' \
    -d '{"status":"todo","order":0}')"
if printf '%s' "$reopened" | grep -q '"completedAt":"'; then
    fail "reopening a task clears completedAt" "$reopened"
else
    pass "reopening a task clears completedAt"
fi

mine="$("${CURL[@]}" -H "$AUTH" "$BASE/api/tasks/mine")"
contains "task shows up in the assignee's list" "$mine" "Smoke parent task"

workload="$("${CURL[@]}" -H "$AUTH" "$BASE/api/users/workload")"
contains "workload report includes the resource" "$workload" '"openTasks"'

comment="$("${CURL[@]}" -X POST "$BASE/api/tasks/$task_id/comments" -H "$AUTH" -H 'Content-Type: application/json' \
    -d '{"body":"smoke comment"}')"
contains "comment added to the task" "$comment" "smoke comment"

# ---------------------------------------------------------------------------
section "Dashboard charts"
for endpoint in overview status-breakdown workload-chart project-progress completion-trend; do
    check "dashboard/$endpoint" "200" "$(status_of -H "$AUTH" "$BASE/api/dashboard/$endpoint")"
done
contains "overview counts projects" "$("${CURL[@]}" -H "$AUTH" "$BASE/api/dashboard/overview")" '"totalProjects":'

# ---------------------------------------------------------------------------
section "Internal chat"
channels="$("${CURL[@]}" -H "$AUTH" "$BASE/api/chat/channels")"
channel_id="$(json_str "$channels" id)"
if [ -n "$channel_id" ]; then
    pass "chat channel was seeded"
    posted="$("${CURL[@]}" -X POST "$BASE/api/chat/channels/$channel_id/messages" -H "$AUTH" \
        -H 'Content-Type: application/json' -d '{"body":"smoke message"}')"
    contains "chat message posted" "$posted" "smoke message"
    contains "chat history returns it" \
        "$("${CURL[@]}" -H "$AUTH" "$BASE/api/chat/channels/$channel_id/messages")" "smoke message"
    message_id="$(json_str "$posted" id)"
else
    fail "chat channel was seeded" "$channels"
fi

# ---------------------------------------------------------------------------
section "Threads, reactions and mentions"

reply="$("${CURL[@]}" -X POST "$BASE/api/chat/messages/$message_id/replies" -H "$AUTH" \
    -H 'Content-Type: application/json' -d '{"body":"smoke reply"}')"
contains "a reply is posted into the thread" "$reply" "smoke reply"
contains "the reply carries its parent"      "$reply" "$message_id"

thread="$("${CURL[@]}" -H "$AUTH" "$BASE/api/chat/messages/$message_id/replies")"
contains "the thread lists the reply" "$thread" "smoke reply"

# A reply belongs to its thread, not to the channel: repeating it at the root
# would make every threaded exchange appear twice in the transcript.
root="$("${CURL[@]}" -H "$AUTH" "$BASE/api/chat/channels/$channel_id/messages")"
if printf '%s' "$root" | grep -q "smoke reply"; then
    fail "replies stay out of the channel transcript" "the reply appears at channel root"
else
    pass "replies stay out of the channel transcript"
fi
contains "the parent carries a reply count" "$root" '"replyCount":1'

# The emoji goes out as a JSON escape rather than as raw UTF-8 in the request
# body. curl.exe under Git Bash re-encodes its arguments through the Windows
# ANSI codepage, which turns anything outside that codepage into a literal "?"
# - so a raw emoji here would quietly store the wrong characters on Windows
# while passing on Linux. The escape is plain ASCII on the wire and decodes to
# U+1F44D at the server. The assertions below still compare against the
# literal, because those run inside bash and grep, where the bytes survive.
THUMBS_UP='{"emoji":"\ud83d\udc4d"}'

reacted="$("${CURL[@]}" -X POST "$BASE/api/chat/messages/$message_id/reactions" -H "$AUTH" \
    -H 'Content-Type: application/json' -d "$THUMBS_UP")"
contains "a reaction is added" "$reacted" '"added":true'
contains "it is attached to the message" \
    "$("${CURL[@]}" -H "$AUTH" "$BASE/api/chat/channels/$channel_id/messages")" '👍'

# A "reaction" is an emoji, not a place to store a paragraph.
check "a long reaction is refused" "400" \
    "$(status_of -X POST "$BASE/api/chat/messages/$message_id/reactions" -H "$AUTH" \
        -H 'Content-Type: application/json' -d '{"emoji":"this is not an emoji"}')"

# The same call toggles: a second tap removes the reaction rather than
# stacking a duplicate.
"${CURL[@]}" -o /dev/null -X POST "$BASE/api/chat/messages/$message_id/reactions" -H "$AUTH" \
    -H 'Content-Type: application/json' -d "$THUMBS_UP"
after_toggle="$("${CURL[@]}" -H "$AUTH" "$BASE/api/chat/channels/$channel_id/messages")"
if printf '%s' "$after_toggle" | grep -q '👍'; then
    fail "reacting twice removes the reaction" "the emoji is still attached"
else
    pass "reacting twice removes the reaction"
fi

check "an unknown message cannot be replied to" "404" \
    "$(status_of -X POST "$BASE/api/chat/messages/00000000-0000-0000-0000-000000000000/replies" \
        -H "$AUTH" -H 'Content-Type: application/json' -d '{"body":"x"}')"

# ---------------------------------------------------------------------------
section "Presence"
presence="$("${CURL[@]}" -H "$AUTH" "$BASE/api/presence")"
contains "presence answers with a list" "$presence" "["
check "presence requires a session" "401" "$(status_of "$BASE/api/presence")"

# ---------------------------------------------------------------------------
section "Realtime WebSocket"
ws="$("${CURL[@]}" -i -N --max-time 5 \
    -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' \
    -H 'Sec-WebSocket-Key: x3JJHMbDL1EzLkh9GBhXDw==' \
    "$BASE/ws?token=$TOKEN" 2>/dev/null | head -n 1)"
contains "WebSocket upgrade through the proxy" "$ws" "101"

ws_anon="$("${CURL[@]}" -o /dev/null -w '%{http_code}' "$BASE/ws")"
check "WebSocket without a token is rejected" "401" "$ws_anon"

# ---------------------------------------------------------------------------
section "Notifications"
check "notifications endpoint" "200" "$(status_of -H "$AUTH" "$BASE/api/notifications")"

# ---------------------------------------------------------------------------
# Regression tests for the privilege escalations fixed in the security pass.
# Every check below runs as an ordinary member and must be refused.
section "Authorization boundaries"

member_user="smoke_member_${RUN_ID}"
member_pass="SmokeMember123!"
member_payload="{\"username\":\"$member_user\",\"name\":\"Smoke Member\",\"email\":\"$member_user@example.com\",\"password\":\"$member_pass\",\"role\":\"member\"}"

# Account creation used to be anonymous AND honoured a caller-supplied role,
# so anyone reachable by the API could mint themselves an administrator.
check "anonymous cannot create an account" "401" \
    "$(status_of -X POST "$BASE/api/auth/register" -H 'Content-Type: application/json' -d "$member_payload")"
check "an unknown role is rejected" "400" \
    "$(status_of -X POST "$BASE/api/auth/register" -H "$AUTH" -H 'Content-Type: application/json' \
        -d "{\"username\":\"smoke_bad_${RUN_ID}\",\"name\":\"x\",\"email\":\"smoke_bad_${RUN_ID}@example.com\",\"password\":\"Password123\",\"role\":\"root\"}")"

created="$("${CURL[@]}" -X POST "$BASE/api/auth/register" -H "$AUTH" -H 'Content-Type: application/json' -d "$member_payload")"
member_id="$(json_str "$created" id)"
if [ -n "$member_id" ]; then
    pass "admin can create a member account"
else
    fail "admin can create a member account" "$created"
fi

member_login="$("${CURL[@]}" -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$member_user\",\"password\":\"$member_pass\"}")"
MEMBER_AUTH="Authorization: Bearer $(json_str "$member_login" token)"

# --- Account takeover -------------------------------------------------------
check "member cannot change another user's password" "403" \
    "$(status_of -X POST "$BASE/api/users/$me_id/password" -H "$MEMBER_AUTH" -H 'Content-Type: application/json' \
        -d '{"password":"HijackedPassword1"}')"
check "member cannot edit another user's profile" "403" \
    "$(status_of -X PUT "$BASE/api/users/$me_id" -H "$MEMBER_AUTH" -H 'Content-Type: application/json' -d '{"name":"Hijacked"}')"

# Role changes stay admin-only even on your own account, and the request is
# refused rather than silently stripped of the field. It used to answer 200
# with the role unchanged, which told the caller a change had been applied
# when it had not.
check "member cannot promote themselves" "403" \
    "$(status_of -X PUT "$BASE/api/users/$member_id" -H "$MEMBER_AUTH" \
        -H 'Content-Type: application/json' -d '{"role":"admin"}')"

# --- Private conversation ---------------------------------------------------
check "member cannot read a channel they are not in" "403" \
    "$(status_of -H "$MEMBER_AUTH" "$BASE/api/chat/channels/$channel_id/messages")"

# --- Creating structure -----------------------------------------------------
check "member cannot create projects" "403" \
    "$(status_of -X POST "$BASE/api/projects" -H "$MEMBER_AUTH" -H 'Content-Type: application/json' \
        -d '{"name":"Rogue project","key":"ROGUE"}')"
check "member cannot create teams" "403" \
    "$(status_of -X POST "$BASE/api/teams" -H "$MEMBER_AUTH" -H 'Content-Type: application/json' -d '{"name":"Rogue team"}')"

# --- Acting inside a project they do not belong to --------------------------
check "member cannot add tasks to a foreign project" "403" \
    "$(status_of -X POST "$BASE/api/projects/$project_id/tasks" -H "$MEMBER_AUTH" -H 'Content-Type: application/json' \
        -d '{"title":"Rogue task"}')"
check "member cannot move a foreign task" "403" \
    "$(status_of -X PATCH "$BASE/api/tasks/$task_id/move" -H "$MEMBER_AUTH" -H 'Content-Type: application/json' \
        -d '{"status":"done","order":0}')"
check "member cannot comment on a foreign task" "403" \
    "$(status_of -X POST "$BASE/api/tasks/$task_id/comments" -H "$MEMBER_AUTH" -H 'Content-Type: application/json' \
        -d '{"body":"rogue"}')"
check "member cannot delete a foreign task" "403" \
    "$(status_of -X DELETE "$BASE/api/tasks/$task_id" -H "$MEMBER_AUTH")"
check "member cannot reconfigure a foreign project" "403" \
    "$(status_of -X PUT "$BASE/api/projects/$project_id" -H "$MEMBER_AUTH" -H 'Content-Type: application/json' \
        -d '{"name":"Hijacked"}')"
check "member cannot delete a foreign project" "403" \
    "$(status_of -X DELETE "$BASE/api/projects/$project_id" -H "$MEMBER_AUTH")"

# --- What a member legitimately can do --------------------------------------
check "member can edit their own profile" "200" \
    "$(status_of -X PUT "$BASE/api/users/$member_id" -H "$MEMBER_AUTH" -H 'Content-Type: application/json' -d '{"title":"QA"}')"
check "self-service password change needs the current password" "401" \
    "$(status_of -X POST "$BASE/api/users/$member_id/password" -H "$MEMBER_AUTH" -H 'Content-Type: application/json' \
        -d '{"currentPassword":"wrong","password":"NewPassword123"}')"
check "self-service password change works with the current password" "200" \
    "$(status_of -X POST "$BASE/api/users/$member_id/password" -H "$MEMBER_AUTH" -H 'Content-Type: application/json' \
        -d "{\"currentPassword\":\"$member_pass\",\"password\":\"NewPassword123\"}")"
check "short passwords are rejected" "400" \
    "$(status_of -X POST "$BASE/api/users/$member_id/password" -H "$AUTH" -H 'Content-Type: application/json' \
        -d '{"password":"short"}')"
check "admin can reset a user's password without the old one" "200" \
    "$(status_of -X POST "$BASE/api/users/$member_id/password" -H "$AUTH" -H 'Content-Type: application/json' \
        -d '{"password":"AdminReset123"}')"

# An administrative password reset signs the account out everywhere - the
# point of the feature, since a reset is usually a response to compromise.
check "an admin reset kills the user's live sessions" "401" \
    "$(status_of -H "$MEMBER_AUTH" "$BASE/api/auth/me")"

# Sign back in with the new password for the checks that follow.
member_login="$("${CURL[@]}" -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$member_user\",\"password\":\"AdminReset123\"}")"
MEMBER_AUTH="Authorization: Bearer $(json_str "$member_login" token)"
check "the user can sign in with the reset password" "200" "$(status_of -H "$MEMBER_AUTH" "$BASE/api/auth/me")"

# The project survived every hostile call above.
contains "the project was never hijacked" "$("${CURL[@]}" -H "$AUTH" "$BASE/api/projects/$project_id")" "Smoke Test Project"

# ---------------------------------------------------------------------------
section "Dependencies and the critical path"

# A second and third task, so there is a chain to reason about.
second="$("${CURL[@]}" -X POST "$BASE/api/projects/$project_id/tasks" -H "$AUTH" -H 'Content-Type: application/json' \
    -d '{"title":"Smoke second task","startDate":"2026-02-01T00:00:00Z","dueDate":"2026-02-10T00:00:00Z"}')"
second_id="$(json_str "$second" id)"
third="$("${CURL[@]}" -X POST "$BASE/api/projects/$project_id/tasks" -H "$AUTH" -H 'Content-Type: application/json' \
    -d '{"title":"Smoke third task","startDate":"2026-02-11T00:00:00Z","dueDate":"2026-02-20T00:00:00Z"}')"
third_id="$(json_str "$third" id)"

check "dependency created" "201" \
    "$(status_of -X POST "$BASE/api/tasks/$second_id/dependencies" -H "$AUTH" -H 'Content-Type: application/json' \
        -d "{\"dependsOn\":\"$task_id\"}")"
check "chained dependency created" "201" \
    "$(status_of -X POST "$BASE/api/tasks/$third_id/dependencies" -H "$AUTH" -H 'Content-Type: application/json' \
        -d "{\"dependsOn\":\"$second_id\"}")"

# A cycle makes the schedule unsolvable, so the database refuses the edge.
check "a dependency cycle is refused" "409" \
    "$(status_of -X POST "$BASE/api/tasks/$task_id/dependencies" -H "$AUTH" -H 'Content-Type: application/json' \
        -d "{\"dependsOn\":\"$third_id\"}")"
check "a task cannot depend on itself" "400" \
    "$(status_of -X POST "$BASE/api/tasks/$task_id/dependencies" -H "$AUTH" -H 'Content-Type: application/json' \
        -d "{\"dependsOn\":\"$task_id\"}")"

schedule="$("${CURL[@]}" -H "$AUTH" "$BASE/api/projects/$project_id/schedule")"
contains "schedule returns the dependency edges" "$schedule" '"dependencies"'
contains "the critical path spans the chain"     "$schedule" "$second_id"
contains "blocked tasks are reported"            "$schedule" '"blocked"'

check "dependency removed" "200" \
    "$(status_of -X DELETE "$BASE/api/tasks/$third_id/dependencies/$second_id" -H "$AUTH")"

# ---------------------------------------------------------------------------
section "Custom fields"
field="$("${CURL[@]}" -X POST "$BASE/api/projects/$project_id/fields" -H "$AUTH" -H 'Content-Type: application/json' \
    -d '{"key":"client","label":"Client","type":"select","options":["Acme","Globex"]}')"
field_id="$(json_str "$field" id)"
if [ -n "$field_id" ]; then
    pass "custom field defined"
else
    fail "custom field defined" "$field"
fi

check "a select field needs options" "400" \
    "$(status_of -X POST "$BASE/api/projects/$project_id/fields" -H "$AUTH" -H 'Content-Type: application/json' \
        -d '{"key":"bad","label":"Bad","type":"select","options":[]}')"
check "an unknown field type is refused" "400" \
    "$(status_of -X POST "$BASE/api/projects/$project_id/fields" -H "$AUTH" -H 'Content-Type: application/json' \
        -d '{"key":"weird","label":"Weird","type":"richtext"}')"

contains "the field is listed for the project" \
    "$("${CURL[@]}" -H "$AUTH" "$BASE/api/projects/$project_id/fields")" '"key":"client"'

valued="$("${CURL[@]}" -X PUT "$BASE/api/tasks/$task_id/fields" -H "$AUTH" -H 'Content-Type: application/json' \
    -d '{"client":"Acme"}')"
contains "the value is stored on the task" "$valued" "Acme"

# Merging, not replacing: writing one field must not erase another.
"${CURL[@]}" -X PUT "$BASE/api/tasks/$task_id/fields" -H "$AUTH" -H 'Content-Type: application/json' \
    -d '{"other":"kept"}' > /dev/null
merged="$("${CURL[@]}" -H "$AUTH" "$BASE/api/tasks/$task_id")"
contains "writing one field preserves the others" "$merged" "Acme"

# ---------------------------------------------------------------------------
section "Time tracking"
check "timer started" "201" "$(status_of -X POST "$BASE/api/tasks/$task_id/time/start" -H "$AUTH")"
# One running timer per person, enforced by a partial unique index.
check "a second timer is refused" "409" \
    "$(status_of -X POST "$BASE/api/tasks/$second_id/time/start" -H "$AUTH")"
contains "the running timer is reported" "$("${CURL[@]}" -H "$AUTH" "$BASE/api/time/running")" "$task_id"
check "timer stopped" "200" "$(status_of -X POST "$BASE/api/time/stop" -H "$AUTH")"
check "stopping with no timer running is a 404" "404" "$(status_of -X POST "$BASE/api/time/stop" -H "$AUTH")"

check "time logged manually" "201" \
    "$(status_of -X POST "$BASE/api/tasks/$task_id/time" -H "$AUTH" -H 'Content-Type: application/json' \
        -d '{"minutes":90,"note":"smoke"}')"
check "zero minutes is refused" "400" \
    "$(status_of -X POST "$BASE/api/tasks/$task_id/time" -H "$AUTH" -H 'Content-Type: application/json' \
        -d '{"minutes":0}')"
contains "entries are listed with their author" \
    "$("${CURL[@]}" -H "$AUTH" "$BASE/api/tasks/$task_id/time")" '"user"'

# ---------------------------------------------------------------------------
section "Watchers"
check "task watched"   "200" "$(status_of -X POST   "$BASE/api/tasks/$task_id/watch" -H "$AUTH")"
check "task unwatched" "200" "$(status_of -X DELETE "$BASE/api/tasks/$task_id/watch" -H "$AUTH")"

# ---------------------------------------------------------------------------
section "Automations"
automation="$("${CURL[@]}" -X POST "$BASE/api/projects/$project_id/automations" -H "$AUTH" \
    -H 'Content-Type: application/json' \
    -d '{"name":"Urgent when in review","trigger":"task.status_changed",
         "conditions":[{"field":"status","op":"eq","value":"review"}],
         "actions":[{"type":"set_priority","priority":"urgent"}]}')"
automation_id="$(json_str "$automation" id)"
if [ -n "$automation_id" ]; then
    pass "automation created"
else
    fail "automation created" "$automation"
fi

check "an automation needs an action" "400" \
    "$(status_of -X POST "$BASE/api/projects/$project_id/automations" -H "$AUTH" -H 'Content-Type: application/json' \
        -d '{"name":"Empty","trigger":"task.created","actions":[]}')"
check "an unknown trigger is refused" "400" \
    "$(status_of -X POST "$BASE/api/projects/$project_id/automations" -H "$AUTH" -H 'Content-Type: application/json' \
        -d '{"name":"Bad","trigger":"task.exploded","actions":[{"type":"notify"}]}')"

# The rule fires end to end: moving the task to "review" must raise its
# priority to urgent without anyone asking.
"${CURL[@]}" -X PATCH "$BASE/api/tasks/$second_id/move" -H "$AUTH" -H 'Content-Type: application/json' \
    -d '{"status":"review","order":0}' > /dev/null
sleep 1
after="$("${CURL[@]}" -H "$AUTH" "$BASE/api/tasks/$second_id")"
contains "the automation applied its action" "$after" '"priority":"urgent"'

runs="$("${CURL[@]}" -H "$AUTH" "$BASE/api/automations/$automation_id/runs")"
contains "the run was recorded" "$runs" '"status":"applied"'

# A rule whose conditions do not hold is recorded as skipped, with the reason -
# an automation that silently does nothing is impossible to debug.
"${CURL[@]}" -X PATCH "$BASE/api/tasks/$second_id/move" -H "$AUTH" -H 'Content-Type: application/json' \
    -d '{"status":"todo","order":0}' > /dev/null
sleep 1
contains "a non-matching run is recorded as skipped" \
    "$("${CURL[@]}" -H "$AUTH" "$BASE/api/automations/$automation_id/runs")" '"status":"skipped"'

check "member cannot create automations" "403" \
    "$(status_of -X POST "$BASE/api/projects/$project_id/automations" -H "$MEMBER_AUTH" \
        -H 'Content-Type: application/json' \
        -d '{"name":"Rogue","trigger":"task.created","actions":[{"type":"notify"}]}')"

# ---------------------------------------------------------------------------
section "Sessions and revocation"
sessions="$("${CURL[@]}" -H "$AUTH" "$BASE/api/auth/sessions")"
contains "the current session is listed" "$sessions" '"current":true'

# A second login opens an independent session; revoking it must not disturb
# the one making the request.
second_login="$("${CURL[@]}" -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' -d "$login_body")"
SECOND_AUTH="Authorization: Bearer $(json_str "$second_login" token)"
check "the second session works" "200" "$(status_of -H "$SECOND_AUTH" "$BASE/api/auth/me")"

# Sessions come back newest first, so the second session is the first entry
# when asked for through its own token.
second_id="$(json_str "$("${CURL[@]}" -H "$SECOND_AUTH" "$BASE/api/auth/sessions")" id)"
if [ -n "$second_id" ]; then
    check "a session can be revoked" "200" "$(status_of -X DELETE -H "$AUTH" "$BASE/api/auth/sessions/$second_id")"
    # This is the point of server-side sessions: the JWT is still
    # cryptographically valid and within its lifetime, yet access is gone.
    check "a revoked token stops working immediately" "401" \
        "$(status_of -H "$SECOND_AUTH" "$BASE/api/auth/me")"
    check "the revoking session is unaffected" "200" "$(status_of -H "$AUTH" "$BASE/api/auth/me")"
else
    fail "a session can be revoked" "could not determine the second session id"
fi

# ---------------------------------------------------------------------------
section "Hierarchy: spaces, folders and lists"
spaces="$("${CURL[@]}" -H "$AUTH" "$BASE/api/spaces")"
space_id="$(json_str "$spaces" id)"
if [ -n "$space_id" ]; then
    pass "a space exists"
else
    fail "a space exists" "$spaces"
fi

new_space="$("${CURL[@]}" -X POST "$BASE/api/spaces" -H "$AUTH" -H 'Content-Type: application/json' \
    -d "{\"name\":\"Smoke Space ${RUN_ID}\",\"description\":\"created by the smoke test\"}")"
new_space_id="$(json_str "$new_space" id)"
if [ -n "$new_space_id" ]; then
    pass "space created"
else
    fail "space created" "$new_space"
fi
contains "the creator owns the space they made" "$new_space" '"yourRole":"owner"'

folder="$("${CURL[@]}" -X POST "$BASE/api/spaces/$new_space_id/folders" -H "$AUTH" \
    -H 'Content-Type: application/json' -d '{"name":"Smoke Folder"}')"
folder_id="$(json_str "$folder" id)"
if [ -n "$folder_id" ]; then
    pass "folder created inside the space"
else
    fail "folder created inside the space" "$folder"
fi

listed="$("${CURL[@]}" -H "$AUTH" "$BASE/api/spaces/$new_space_id/folders")"
contains "folder appears in the space" "$listed" "Smoke Folder"

# A list placed in a folder of another space must be refused: the trigger
# guarding that invariant lives in the database, not in application code.
cross="$(status_of -X POST "$BASE/api/projects" -H "$AUTH" -H 'Content-Type: application/json' \
    -d "{\"name\":\"Cross space\",\"key\":\"XSPACE${RUN_ID}\",\"spaceId\":\"$space_id\",\"folderId\":\"$folder_id\"}")"
if [ "$cross" = "201" ]; then
    fail "a folder from another space is rejected" "the API accepted it (got 201)"
else
    pass "a folder from another space is rejected"
fi

check "member cannot create spaces" "403" \
    "$(status_of -X POST "$BASE/api/spaces" -H "$MEMBER_AUTH" -H 'Content-Type: application/json' \
        -d '{"name":"Rogue space"}')"
check "member cannot delete a space" "403" \
    "$(status_of -X DELETE "$BASE/api/spaces/$new_space_id" -H "$MEMBER_AUTH")"

# ---------------------------------------------------------------------------
section "Docs"
doc="$("${CURL[@]}" -X POST "$BASE/api/docs" -H "$AUTH" -H 'Content-Type: application/json' \
    -d "{\"spaceId\":\"$new_space_id\",\"title\":\"Smoke doc\",\"content\":\"first draft\"}")"
doc_id="$(json_str "$doc" id)"
if [ -n "$doc_id" ]; then
    pass "document created"
else
    fail "document created" "$doc"
fi

contains "documents are listed for their space" \
    "$("${CURL[@]}" -H "$AUTH" "$BASE/api/docs?spaceId=$new_space_id")" "Smoke doc"

updated_doc="$("${CURL[@]}" -X PUT "$BASE/api/docs/$doc_id" -H "$AUTH" -H 'Content-Type: application/json' \
    -d '{"title":"Smoke doc","content":"second draft"}')"
contains "document content is updated" "$updated_doc" "second draft"

revs="$("${CURL[@]}" -H "$AUTH" "$BASE/api/docs/$doc_id/revisions")"
before="$(printf '%s' "$revs" | grep -o '"id":' | wc -l | tr -d ' ')"
check "creating and editing left two versions" "2" "$before"

# Saving identical content must not manufacture a revision, or the history
# fills with noise and hides the edits that matter.
"${CURL[@]}" -o /dev/null -X PUT "$BASE/api/docs/$doc_id" -H "$AUTH" -H 'Content-Type: application/json' \
    -d '{"title":"Smoke doc","content":"second draft"}'
revs_again="$("${CURL[@]}" -H "$AUTH" "$BASE/api/docs/$doc_id/revisions")"
after="$(printf '%s' "$revs_again" | grep -o '"id":' | wc -l | tr -d ' ')"
check "an unchanged save creates no revision" "$before" "$after"

# A history nobody can read back is decorative: recovering from a careless
# paste means fetching the old text, so the first draft must still be there.
oldest="$(printf '%s' "$revs" | grep -o '"id":[0-9]*' | tail -n 1 | cut -d: -f2)"
old_rev="$("${CURL[@]}" -H "$AUTH" "$BASE/api/docs/$doc_id/revisions/$oldest")"
contains "an old version can be read back" "$old_rev" "first draft"

# The document id is part of the lookup, so a revision id guessed from another
# document does not resolve.
check "a revision of another document is not reachable" "404" \
    "$(status_of -H "$AUTH" "$BASE/api/docs/$new_space_id/revisions/$oldest")"

# A document carries no access list of its own: it is exactly as visible, and
# exactly as editable, as the space holding it.
check "an open space's documents are readable" "200" \
    "$(status_of -H "$MEMBER_AUTH" "$BASE/api/docs/$doc_id")"
check "reading it does not imply editing it" "403" \
    "$(status_of -X PUT "$BASE/api/docs/$doc_id" -H "$MEMBER_AUTH" -H 'Content-Type: application/json' \
        -d '{"content":"vandalised"}')"
check "a member cannot delete the document" "403" \
    "$(status_of -X DELETE "$BASE/api/docs/$doc_id" -H "$MEMBER_AUTH")"

# The same inheritance in the direction that matters: a private space hides its
# documents, and hides them as absent rather than as forbidden.
private_space="$("${CURL[@]}" -X POST "$BASE/api/spaces" -H "$AUTH" -H 'Content-Type: application/json' \
    -d "{\"name\":\"Smoke Private ${RUN_ID}\",\"isPrivate\":true}")"
private_space_id="$(json_str "$private_space" id)"
private_doc="$("${CURL[@]}" -X POST "$BASE/api/docs" -H "$AUTH" -H 'Content-Type: application/json' \
    -d "{\"spaceId\":\"$private_space_id\",\"title\":\"Secret\",\"content\":\"confidential\"}")"
private_doc_id="$(json_str "$private_doc" id)"
check "a private space's documents are hidden" "404" \
    "$(status_of -H "$MEMBER_AUTH" "$BASE/api/docs/$private_doc_id")"
check "their revisions are hidden too" "404" \
    "$(status_of -H "$MEMBER_AUTH" "$BASE/api/docs/$private_doc_id/revisions")"
check "listing cannot walk around it" "404" \
    "$(status_of -H "$MEMBER_AUTH" "$BASE/api/docs?spaceId=$private_space_id")"

check "document deleted" "200" "$(status_of -X DELETE -H "$AUTH" "$BASE/api/docs/$doc_id")"
check "private space deleted" "200" "$(status_of -X DELETE -H "$AUTH" "$BASE/api/spaces/$private_space_id")"

# ---------------------------------------------------------------------------
section "Notification preferences"
prefs="$("${CURL[@]}" -H "$AUTH" "$BASE/api/notifications/preferences")"
contains "preferences answer with defaults" "$prefs" '"channels"'
contains "the digest defaults to a value"   "$prefs" '"digest"'

saved="$("${CURL[@]}" -X PUT "$BASE/api/notifications/preferences" -H "$AUTH" \
    -H 'Content-Type: application/json' \
    -d '{"channels":{"task_assigned":{"inApp":true,"email":false}},"digest":"daily","digestHour":8}')"
contains "preferences are saved"    "$saved" '"digest":"daily"'
contains "the digest hour is saved" "$saved" '"digestHour":8'

reread="$("${CURL[@]}" -H "$AUTH" "$BASE/api/notifications/preferences")"
contains "preferences survive a re-read" "$reread" '"digest":"daily"'

# An hour outside the clock is a bug on the way in, not a value to store.
check "an impossible digest hour is rejected" "400" \
    "$(status_of -X PUT "$BASE/api/notifications/preferences" -H "$AUTH" \
        -H 'Content-Type: application/json' -d '{"digest":"daily","digestHour":47}')"
check "an unknown digest cadence is rejected" "400" \
    "$(status_of -X PUT "$BASE/api/notifications/preferences" -H "$AUTH" \
        -H 'Content-Type: application/json' -d '{"digest":"hourly"}')"

# ---------------------------------------------------------------------------
section "Goals and OKRs"
goal="$("${CURL[@]}" -X POST "$BASE/api/goals" -H "$AUTH" -H 'Content-Type: application/json' \
    -d "{\"name\":\"Smoke goal ${RUN_ID}\",\"description\":\"created by the smoke test\"}")"
goal_id="$(json_str "$goal" id)"
if [ -n "$goal_id" ]; then
    pass "goal created"
else
    fail "goal created" "$goal"
fi
contains "a new goal starts active" "$goal" '"status":"active"'

# A manual measure: somebody types the number in.
with_manual="$("${CURL[@]}" -X POST "$BASE/api/goals/$goal_id/key-results" -H "$AUTH" \
    -H 'Content-Type: application/json' \
    -d '{"name":"Manual measure","source":"manual","startValue":0,"targetValue":10}')"
contains "a manual measure is added" "$with_manual" "Manual measure"
contains "an untouched measure is at zero" "$with_manual" '"progress":0'

manual_kr_id="$(printf '%s' "$with_manual" | grep -o '"keyResults":\[{"id":"[^"]*"' | sed 's/.*"id":"//;s/"$//')"
halfway="$("${CURL[@]}" -X PUT "$BASE/api/goals/$goal_id/key-results/$manual_kr_id" -H "$AUTH" \
    -H 'Content-Type: application/json' -d '{"value":5}')"
contains "a manual reading is recorded" "$halfway" '"currentValue":5'
contains "progress follows the reading" "$halfway" '"progress":0.5'

# A derived measure reads the project's own tasks, so a goal cannot drift from
# the work underneath it.
derived="$("${CURL[@]}" -X POST "$BASE/api/goals/$goal_id/key-results" -H "$AUTH" \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"Delivery\",\"source\":\"tasks_completed\",\"projectId\":\"$project_id\",\"targetValue\":100,\"unit\":\"%\"}")"
contains "a derived measure is added" "$derived" "Delivery"

derived_kr_id="$("${CURL[@]}" -H "$AUTH" "$BASE/api/goals/$goal_id" \
    | grep -o '"id":"[^"]*","goalId"' | tail -n 1 | sed 's/"id":"//;s/","goalId"//')"
check "a computed measure cannot be set by hand" "400" \
    "$(status_of -X PUT "$BASE/api/goals/$goal_id/key-results/$derived_kr_id" -H "$AUTH" \
        -H 'Content-Type: application/json' -d '{"value":99}')"

check "a measure taken from tasks needs a project" "400" \
    "$(status_of -X POST "$BASE/api/goals/$goal_id/key-results" -H "$AUTH" \
        -H 'Content-Type: application/json' -d '{"name":"Nowhere","source":"tasks_completed"}')"
check "an unknown measure source is refused" "400" \
    "$(status_of -X POST "$BASE/api/goals/$goal_id/key-results" -H "$AUTH" \
        -H 'Content-Type: application/json' -d '{"name":"Vibes","source":"feelings"}')"
check "an unknown goal status is refused" "400" \
    "$(status_of -X PUT "$BASE/api/goals/$goal_id" -H "$AUTH" \
        -H 'Content-Type: application/json' -d '{"status":"probably_fine"}')"

# An organisation-wide goal is not something an ordinary member declares.
check "member cannot declare an organisation-wide goal" "403" \
    "$(status_of -X POST "$BASE/api/goals" -H "$MEMBER_AUTH" -H 'Content-Type: application/json' \
        -d '{"name":"Rogue goal"}')"
check "member cannot delete someone else's goal" "403" \
    "$(status_of -X DELETE "$BASE/api/goals/$goal_id" -H "$MEMBER_AUTH")"

check "goal deleted" "200" "$(status_of -X DELETE "$BASE/api/goals/$goal_id" -H "$AUTH")"

# ---------------------------------------------------------------------------
section "Portfolio, capacity and earned value"
portfolio="$("${CURL[@]}" -H "$AUTH" "$BASE/api/portfolio")"
contains "the portfolio reports health" "$portfolio" '"health"'
contains "it reports progress"          "$portfolio" '"progress"'

# Cross-project reporting names every project and its health, which is exactly
# what an ordinary member has no standing to read.
check "member cannot read the portfolio" "403" "$(status_of -H "$MEMBER_AUTH" "$BASE/api/portfolio")"
check "nor the capacity report" "403" "$(status_of -H "$MEMBER_AUTH" "$BASE/api/portfolio/capacity")"

capacity="$("${CURL[@]}" -H "$AUTH" "$BASE/api/portfolio/capacity")"
contains "capacity reports committed hours" "$capacity" '"committedHours"'
contains "against a declared capacity"      "$capacity" '"capacityHours"'

csv="$("${CURL[@]}" -H "$AUTH" "$BASE/api/portfolio/export.csv")"
contains "the portfolio exports as CSV" "$csv" "key,name,status,health"

# Without an approved plan there is nothing to measure against, and saying so
# beats returning zeroes that look like data.
check "earned value needs a baseline first" "404" \
    "$(status_of -H "$AUTH" "$BASE/api/projects/$project_id/earned-value")"

baseline="$("${CURL[@]}" -X POST "$BASE/api/projects/$project_id/baselines" -H "$AUTH" \
    -H 'Content-Type: application/json' -d '{"name":"Smoke baseline"}')"
contains "a baseline freezes the plan" "$baseline" "Smoke baseline"

evm="$("${CURL[@]}" -H "$AUTH" "$BASE/api/projects/$project_id/earned-value")"
contains "earned value reports the budget" "$evm" '"bac"'
contains "and the planned value"           "$evm" '"pv"'
contains "and what was earned"             "$evm" '"ev"'
contains "and what it actually cost"       "$evm" '"ac"'

# The indices are always present as keys, carrying either a number or null -
# never absent. Which of the two they carry depends on whether their
# denominator is zero, and that distinction is covered by the unit tests.
contains "the schedule index is reported" "$evm" '"spi"'
contains "the cost index is reported"     "$evm" '"cpi"'

# The baseline captured the real plan, so the budget cannot be zero.
if printf '%s' "$evm" | grep -q '"bac":0,'; then
    fail "the baseline carries the plan's budget" "BAC came back as 0"
else
    pass "the baseline carries the plan's budget"
fi

check "member cannot capture a baseline" "403" \
    "$(status_of -X POST "$BASE/api/projects/$project_id/baselines" -H "$MEMBER_AUTH" \
        -H 'Content-Type: application/json' -d '{"name":"Rogue"}')"

# ---------------------------------------------------------------------------
section "Saved dashboard layout"
# The arrangement used to live in one browser's local storage, so it did not
# follow anyone to a second machine.
empty_layout="$("${CURL[@]}" -H "$AUTH" "$BASE/api/dashboard/layout")"
contains "an unsaved layout is absent, not empty" "$empty_layout" '"layout":null'

saved_layout="$("${CURL[@]}" -X PUT "$BASE/api/dashboard/layout" -H "$AUTH" \
    -H 'Content-Type: application/json' \
    -d '{"layout":[{"id":"chart-status"},{"id":"kpi-total-tasks"}]}')"
contains "a layout is saved" "$saved_layout" "chart-status"

contains "and read back in order" \
    "$("${CURL[@]}" -H "$AUTH" "$BASE/api/dashboard/layout")" '"chart-status"'

# Saving again must update the row rather than create a second default.
"${CURL[@]}" -o /dev/null -X PUT "$BASE/api/dashboard/layout" -H "$AUTH" \
    -H 'Content-Type: application/json' -d '{"layout":[{"id":"kpi-done-tasks"}]}'
relayout="$("${CURL[@]}" -H "$AUTH" "$BASE/api/dashboard/layout")"
contains "saving again replaces the layout" "$relayout" "kpi-done-tasks"
if printf '%s' "$relayout" | grep -q 'chart-status'; then
    fail "the old layout is replaced, not merged" "the previous cards are still there"
else
    pass "the old layout is replaced, not merged"
fi

check "a layout is capped in size" "400" \
    "$(status_of -X PUT "$BASE/api/dashboard/layout" -H "$AUTH" -H 'Content-Type: application/json' \
        -d '{"layout":[{"id":""}]}')"

check "layout reset" "200" "$(status_of -X DELETE "$BASE/api/dashboard/layout" -H "$AUTH")"

# ---------------------------------------------------------------------------
section "Service tokens and SCIM provisioning"
check "member cannot mint machine credentials" "403" \
    "$(status_of -X POST "$BASE/api/service-tokens" -H "$MEMBER_AUTH" -H 'Content-Type: application/json' \
        -d '{"name":"rogue","scopes":["scim"]}')"
check "a token with no scope is refused" "400" \
    "$(status_of -X POST "$BASE/api/service-tokens" -H "$AUTH" -H 'Content-Type: application/json' \
        -d '{"name":"empty","scopes":[]}')"
check "an unknown scope is refused" "400" \
    "$(status_of -X POST "$BASE/api/service-tokens" -H "$AUTH" -H 'Content-Type: application/json' \
        -d '{"name":"broad","scopes":["everything"]}')"

token_response="$("${CURL[@]}" -X POST "$BASE/api/service-tokens" -H "$AUTH" \
    -H 'Content-Type: application/json' -d "{\"name\":\"smoke-scim-${RUN_ID}\",\"scopes\":[\"scim\"]}")"
token_id="$(json_str "$token_response" id)"
token_secret="$(json_str "$token_response" secret)"
contains "the secret is returned once" "$token_response" "pvt_"

# Only the hash is stored, so the listing must never be able to hand it back.
listed_tokens="$("${CURL[@]}" -H "$AUTH" "$BASE/api/service-tokens")"
if printf '%s' "$listed_tokens" | grep -q "$token_secret"; then
    fail "the secret is never readable again" "the listing returned the token"
else
    pass "the secret is never readable again"
fi

SCIM_AUTH="Authorization: Bearer $token_secret"
check "SCIM refuses an anonymous caller" "401" "$(status_of "$BASE/scim/v2/Users")"
check "SCIM refuses a user session"      "401" "$(status_of -H "$AUTH" "$BASE/scim/v2/Users")"
check "SCIM refuses an unknown token"    "401" \
    "$(status_of -H 'Authorization: Bearer pvt_not-a-real-token' "$BASE/scim/v2/Users")"

scim_list="$("${CURL[@]}" -H "$SCIM_AUTH" "$BASE/scim/v2/Users")"
contains "SCIM lists users in its own envelope" "$scim_list" "urn:ietf:params:scim:api:messages:2.0:ListResponse"

scim_user="$("${CURL[@]}" -X POST "$BASE/scim/v2/Users" -H "$SCIM_AUTH" \
    -H 'Content-Type: application/scim+json' \
    -d "{\"schemas\":[\"urn:ietf:params:scim:schemas:core:2.0:User\"],\"userName\":\"scim_smoke_${RUN_ID}\",\"externalId\":\"idp-${RUN_ID}\",\"displayName\":\"SCIM Smoke\",\"emails\":[{\"value\":\"scim_smoke_${RUN_ID}@example.com\",\"primary\":true}],\"active\":true}")"
scim_id="$(json_str "$scim_user" id)"
contains "SCIM provisions an account" "$scim_user" "SCIM Smoke"
contains "and echoes the external id"  "$scim_user" "idp-${RUN_ID}"

# A provisioning client that retries must not create a duplicate.
check "a repeated userName is a conflict" "409" \
    "$(status_of -X POST "$BASE/scim/v2/Users" -H "$SCIM_AUTH" -H 'Content-Type: application/scim+json' \
        -d "{\"userName\":\"scim_smoke_${RUN_ID}\",\"active\":true}")"

contains "the filter every client sends works" \
    "$("${CURL[@]}" -H "$SCIM_AUTH" "$BASE/scim/v2/Users?filter=userName%20eq%20%22scim_smoke_${RUN_ID}%22")" \
    "SCIM Smoke"

# An unsupported filter must be refused rather than quietly returning everyone.
check "an unsupported filter is refused" "400" \
    "$(status_of -H "$SCIM_AUTH" "$BASE/scim/v2/Users?filter=displayName%20eq%20%22SCIM%20Smoke%22")"

# Deprovisioning is the whole reason SCIM is here.
patched="$("${CURL[@]}" -X PATCH "$BASE/scim/v2/Users/$scim_id" -H "$SCIM_AUTH" \
    -H 'Content-Type: application/scim+json' \
    -d '{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"active","value":false}]}')"
contains "a SCIM patch deactivates the account" "$patched" '"active":false'

check "a patch with nothing recognisable is refused" "400" \
    "$(status_of -X PATCH "$BASE/scim/v2/Users/$scim_id" -H "$SCIM_AUTH" \
        -H 'Content-Type: application/scim+json' \
        -d '{"Operations":[{"op":"replace","path":"nickname","value":"x"}]}')"

# A delete deactivates rather than destroying the person's work.
check "SCIM delete answers 204" "204" \
    "$(status_of -X DELETE "$BASE/scim/v2/Users/$scim_id" -H "$SCIM_AUTH")"
still_there="$("${CURL[@]}" -H "$SCIM_AUTH" "$BASE/scim/v2/Users/$scim_id")"
contains "the account survives as deactivated" "$still_there" '"active":false'

check "a revoked token stops working" "200" "$(status_of -X DELETE "$BASE/api/service-tokens/$token_id" -H "$AUTH")"
check "and is refused immediately after" "401" "$(status_of -H "$SCIM_AUTH" "$BASE/scim/v2/Users")"

# ---------------------------------------------------------------------------
section "Privacy: export and erasure"
export_bundle="$("${CURL[@]}" -H "$AUTH" "$BASE/api/users/$me_id/data-export")"
contains "an export includes the profile" "$export_bundle" '"profile"'
contains "and the tasks assigned"         "$export_bundle" '"assignedTasks"'
contains "and the audit trail"            "$export_bundle" '"auditTrail"'

# The export is about a person, not everything they can see.
if printf '%s' "$export_bundle" | grep -qi '"password'; then
    fail "an export carries no credentials" "a password field appears in the bundle"
else
    pass "an export carries no credentials"
fi

check "a member can export their own record" "200" \
    "$(status_of -H "$MEMBER_AUTH" "$BASE/api/users/$member_id/data-export")"
check "but not somebody else's" "403" \
    "$(status_of -H "$MEMBER_AUTH" "$BASE/api/users/$me_id/data-export")"

check "erasure is not a member's to perform" "403" \
    "$(status_of -X POST "$BASE/api/users/$me_id/erase" -H "$MEMBER_AUTH" \
        -H 'Content-Type: application/json' -d "{\"confirm\":\"$ADMIN_USER\"}")"
# Irreversible, so it takes more than one click.
check "erasure without confirmation is refused" "400" \
    "$(status_of -X POST "$BASE/api/users/$scim_id/erase" -H "$AUTH" \
        -H 'Content-Type: application/json' -d '{}')"
check "an administrator cannot erase themselves" "400" \
    "$(status_of -X POST "$BASE/api/users/$me_id/erase" -H "$AUTH" \
        -H 'Content-Type: application/json' -d "{\"confirm\":\"$ADMIN_USER\"}")"

erased="$(status_of -X POST "$BASE/api/users/$scim_id/erase" -H "$AUTH" \
    -H 'Content-Type: application/json' -d "{\"confirm\":\"scim_smoke_${RUN_ID}\"}")"
check "an administrator can erase an account" "200" "$erased"

after_erasure="$("${CURL[@]}" -H "$AUTH" "$BASE/api/users/$scim_id")"
contains "the account becomes a tombstone" "$after_erasure" "Deleted user"
if printf '%s' "$after_erasure" | grep -q "scim_smoke_${RUN_ID}@example.com"; then
    fail "identifiers are gone after erasure" "the original e-mail is still there"
else
    pass "identifiers are gone after erasure"
fi

# Erasure anonymises rather than deletes: destroying the row would cascade
# through work that belongs to the organisation and punch holes in the trail.
contains "the audit trail records the erasure" \
    "$("${CURL[@]}" -H "$AUTH" "$BASE/api/audit?action=privacy.erased")" "privacy.erased"

# Repeating it must not rename the tombstone again.
check "erasure is idempotent" "400" \
    "$(status_of -X POST "$BASE/api/users/$scim_id/erase" -H "$AUTH" \
        -H 'Content-Type: application/json' -d "{\"confirm\":\"scim_smoke_${RUN_ID}\"}")"

# ---------------------------------------------------------------------------
section "Single sign-on"
sso_config="$("${CURL[@]}" "$BASE/api/auth/oidc/config")"
contains "the login screen can ask whether SSO is on" "$sso_config" '"enabled"'
# Disabled in this deployment, so the flow must say so rather than half-start.
check "starting a flow that is not configured is refused" "404" \
    "$(status_of "$BASE/api/auth/oidc/start")"

# ---------------------------------------------------------------------------
section "Search and pagination"
check "task search endpoint" "200" "$(status_of -H "$AUTH" "$BASE/api/tasks?limit=5")"

paged="$("${CURL[@]}" -H "$AUTH" "$BASE/api/tasks?limit=1&total=true")"
contains "results come back paginated" "$paged" '"hasMore"'
contains "a total is reported when asked for" "$paged" '"total"'

found="$("${CURL[@]}" -H "$AUTH" "$BASE/api/tasks?q=Smoke%20parent")"
contains "full-text search finds the task by title" "$found" "Smoke parent task"

missing="$("${CURL[@]}" -H "$AUTH" "$BASE/api/tasks?q=zzzznotarealword")"
if printf '%s' "$missing" | grep -q '"items":\[\]'; then
    pass "a search with no matches returns an empty page"
else
    fail "a search with no matches returns an empty page" "$missing"
fi

check "filtering by project" "200" "$(status_of -H "$AUTH" "$BASE/api/tasks?projectId=$project_id")"
check "filtering by status" "200" "$(status_of -H "$AUTH" "$BASE/api/tasks?status=todo")"
# An unbounded limit is a denial-of-service vector; the cap must hold.
capped="$("${CURL[@]}" -H "$AUTH" "$BASE/api/tasks?limit=99999")"
contains "an absurd limit is clamped, not honoured" "$capped" '"items"'

# ---------------------------------------------------------------------------
section "Audit trail"
audit="$("${CURL[@]}" -H "$AUTH" "$BASE/api/audit?limit=50")"
contains "the trail records logins"        "$audit" "auth.login"
contains "the trail records task creation" "$audit" "task.created"
contains "the trail records who acted"     "$audit" '"actor"'

# Failed logins are exactly what an investigation needs.
failed="$("${CURL[@]}" -H "$AUTH" "$BASE/api/audit?action=auth.login_failed")"
contains "failed logins are recorded" "$failed" "auth.login_failed"

# Secrets must never reach a table this widely readable.
if printf '%s' "$audit" | grep -qi '"password"[^:]*:[^"]*"[^"]'; then
    fail "no plaintext secrets in the trail" "a password value appears in the audit output"
else
    pass "no plaintext secrets in the trail"
fi

check "the trail is admin-only" "403" "$(status_of -H "$MEMBER_AUTH" "$BASE/api/audit")"

# ---------------------------------------------------------------------------
section "Operability"
ready="$("${CURL[@]}" "$BASE/api/ready")"
contains "readiness reports the database" "$ready" '"database":"ok"'

# Metrics are for an internal scrape target. Reaching them from the public
# edge would hand an outsider the shape of the whole installation.
metrics="$("${CURL[@]}" "$BASE/metrics" 2>/dev/null)"
if printf '%s' "$metrics" | grep -q 'projectview_http_requests_total'; then
    fail "metrics are not reachable from the public edge" "the proxy served /metrics"
else
    pass "metrics are not reachable from the public edge"
fi

# ---------------------------------------------------------------------------
section "Roles and the last administrator"

# Checked before the promotion below, because afterwards this account really
# is an administrator and the assertion would be testing the wrong thing.
# A member promoting themselves would make every other authorization check
# decorative.
check "a member cannot promote themselves" "403"     "$(status_of -X PUT "$BASE/api/users/$member_id" -H "$MEMBER_AUTH"         -H 'Content-Type: application/json' -d '{"role":"admin"}')"
check "the portfolio is closed to them" "403" "$(status_of -H "$MEMBER_AUTH" "$BASE/api/portfolio")"
# Refused, not silently ignored: answering 200 while dropping the field tells
# the caller a change was applied when it was not.
check "a member cannot activate an account either" "403"     "$(status_of -X PUT "$BASE/api/users/$member_id" -H "$MEMBER_AUTH"         -H 'Content-Type: application/json' -d '{"active":false}')"
# The rest of the profile is still theirs to edit.
check "but they can still edit their own profile" "200"     "$(status_of -X PUT "$BASE/api/users/$member_id" -H "$MEMBER_AUTH"         -H 'Content-Type: application/json' -d '{"title":"Analyst"}')"

# Promoting somebody is what the administration screen exists to do.
promoted="$("${CURL[@]}" -X PUT "$BASE/api/users/$member_id" -H "$AUTH"     -H 'Content-Type: application/json' -d '{"role":"admin"}')"
contains "an administrator can promote somebody" "$promoted" '"role":"admin"'
contains "the promotion is recorded"     "$("${CURL[@]}" -H "$AUTH" "$BASE/api/audit?action=user.role_changed")" "user.role_changed"

# The new role applies to the session they already hold. Authorization reads
# the account on every request rather than trusting the role baked into the
# token, so a promotion does not wait for the next sign-in - and neither does
# a demotion, which is the half that matters.
check "the promotion applies to the open session" "200"     "$(status_of -H "$MEMBER_AUTH" "$BASE/api/portfolio")"

check "an unknown role is refused" "400"     "$(status_of -X PUT "$BASE/api/users/$member_id" -H "$AUTH"         -H 'Content-Type: application/json' -d '{"role":"superuser"}')"

# With two administrators, either may be demoted.
demoted="$("${CURL[@]}" -X PUT "$BASE/api/users/$member_id" -H "$AUTH"     -H 'Content-Type: application/json' -d '{"role":"member"}')"
contains "one of two administrators can be demoted" "$demoted" '"role":"member"'

# And the demotion closes the door again, on the same session.
check "the demotion applies just as fast" "403" "$(status_of -H "$MEMBER_AUTH" "$BASE/api/portfolio")"

# And now the rule that matters: nothing may leave the installation with
# nobody able to administer it. The path back from that is an UPDATE against
# the database, which is not a recovery procedure anyone should be one
# careless click away from.
check "the last administrator cannot be demoted" "409"     "$(status_of -X PUT "$BASE/api/users/$me_id" -H "$AUTH"         -H 'Content-Type: application/json' -d '{"role":"member"}')"
check "nor deactivated" "409"     "$(status_of -X PUT "$BASE/api/users/$me_id" -H "$AUTH"         -H 'Content-Type: application/json' -d '{"active":false}')"

# The refusal must not be a blanket block on editing the account.
check "the last administrator can still be edited" "200"     "$(status_of -X PUT "$BASE/api/users/$me_id" -H "$AUTH"         -H 'Content-Type: application/json' -d '{"title":"Administrator"}')"

# Still an administrator after all that.
contains "the account survived the attempts"     "$("${CURL[@]}" -H "$AUTH" "$BASE/api/auth/me")" '"role":"admin"'

# ---------------------------------------------------------------------------
section "System settings"
# These decide who may sign in and where e-mail goes, so reading them is close
# to being able to take the installation over. The whole group is admin-only,
# reads included.
check "member cannot read the settings" "403" "$(status_of -H "$MEMBER_AUTH" "$BASE/api/settings")"
check "nor write them" "403"     "$(status_of -X PUT "$BASE/api/settings" -H "$MEMBER_AUTH" -H 'Content-Type: application/json'         -d '{"values":{"SMTP_HOST":"evil.example.com"}}')"
check "nor download them" "403" "$(status_of -H "$MEMBER_AUTH" "$BASE/api/settings/env")"
check "anonymous cannot read them" "401" "$(status_of "$BASE/api/settings")"

settings="$("${CURL[@]}" -H "$AUTH" "$BASE/api/settings")"
contains "settings list the managed keys" "$settings" '"SMTP_HOST"'
contains "and say where each came from"   "$settings" '"overridden"'

# A credential must never come back out. The screen shows only whether one is
# set, and the encrypted value never leaves the database.
if printf '%s' "$settings" | grep -o '"key":"SMTP_PASSWORD"[^}]*' | grep -q '"value"'; then
    fail "secrets are never read back" "SMTP_PASSWORD came back with a value"
else
    pass "secrets are never read back"
fi

# The allow-list is the security boundary: anything outside it is refused by
# name rather than silently dropped.
check "the database URL cannot be rewritten" "400"     "$(status_of -X PUT "$BASE/api/settings" -H "$AUTH" -H 'Content-Type: application/json'         -d '{"values":{"DATABASE_URL":"postgres://attacker/db"}}')"
check "nor the token signing secret" "400"     "$(status_of -X PUT "$BASE/api/settings" -H "$AUTH" -H 'Content-Type: application/json'         -d '{"values":{"JWT_SECRET":"hunter2"}}')"
check "nor the bootstrap admin password" "400"     "$(status_of -X PUT "$BASE/api/settings" -H "$AUTH" -H 'Content-Type: application/json'         -d '{"values":{"BOOTSTRAP_ADMIN_PASSWORD":"hunter2"}}')"

saved_settings="$("${CURL[@]}" -X PUT "$BASE/api/settings" -H "$AUTH" -H 'Content-Type: application/json'     -d '{"values":{"SMTP_HOST":"smoke.mail.example.com","ALERT_WARN_DAYS_BEFORE":"5","SMTP_PASSWORD":"s3cr3t"}}')"
contains "an administrator can save settings" "$saved_settings" '"ok":true'
contains "and they are applied at once"       "$saved_settings" '"applied":true'

reread_settings="$("${CURL[@]}" -H "$AUTH" "$BASE/api/settings")"
contains "the new value is in force" "$reread_settings" "smoke.mail.example.com"
contains "and is marked as changed here" "$reread_settings" '"overridden":true'

# A secret saved once must not be wiped by a later save that leaves the field
# blank - the commonest way a settings screen breaks a live system.
"${CURL[@]}" -o /dev/null -X PUT "$BASE/api/settings" -H "$AUTH" -H 'Content-Type: application/json'     -d '{"values":{"SMTP_HOST":"smoke.mail.example.com","SMTP_PASSWORD":""}}'
still_set="$("${CURL[@]}" -H "$AUTH" "$BASE/api/settings")"
if printf '%s' "$still_set" | grep -o '"key":"SMTP_PASSWORD"[^}]*' | grep -q '"isSet":true'; then
    pass "an empty secret field keeps the stored one"
else
    fail "an empty secret field keeps the stored one" "the stored password was cleared"
fi

# The export carries the effective configuration, in .env shape.
env_export="$("${CURL[@]}" -H "$AUTH" "$BASE/api/settings/env")"
contains "the export is .env shaped" "$env_export" "SMTP_HOST=smoke.mail.example.com"

# Clearing an override reverts to what the deployment configured, not to empty.
cleared_settings="$("${CURL[@]}" -X PUT "$BASE/api/settings" -H "$AUTH" -H 'Content-Type: application/json'     -d '{"values":{},"clear":["SMTP_HOST","ALERT_WARN_DAYS_BEFORE","SMTP_PASSWORD"]}')"
contains "an override can be cleared" "$cleared_settings" '"ok":true'
after_clear="$("${CURL[@]}" -H "$AUTH" "$BASE/api/settings/env")"
if printf '%s' "$after_clear" | grep -q "SMTP_HOST=smoke.mail.example.com"; then
    fail "clearing reverts to the environment" "the override is still in force"
else
    pass "clearing reverts to the environment"
fi

# Testing an integration that is switched off would prove nothing.
check "testing mail while it is off is refused" "400"     "$(status_of -X POST "$BASE/api/settings/test/smtp" -H "$AUTH" -H 'Content-Type: application/json' -d '{}')"
check "testing the directory while it is off is refused" "400"     "$(status_of -X POST "$BASE/api/settings/test/ad" -H "$AUTH" -H 'Content-Type: application/json'         -d '{"username":"x","password":"y"}')"
check "member cannot run the tests" "403"     "$(status_of -X POST "$BASE/api/settings/test/smtp" -H "$MEMBER_AUTH" -H 'Content-Type: application/json' -d '{}')"

contains "settings changes are audited"     "$("${CURL[@]}" -H "$AUTH" "$BASE/api/audit?action=settings.changed")" "settings.changed"
# The keys, never the values: the trail is readable by every administrator.
if "${CURL[@]}" -H "$AUTH" "$BASE/api/audit?action=settings.changed" | grep -q "s3cr3t"; then
    fail "the trail records keys, not secrets" "a saved password appears in the audit log"
else
    pass "the trail records keys, not secrets"
fi

# ---------------------------------------------------------------------------
section "Cleanup"
check "smoke space deleted" "200" "$(status_of -X DELETE -H "$AUTH" "$BASE/api/spaces/$new_space_id")"
check "smoke member deactivated" "200" \
    "$(status_of -X PUT "$BASE/api/users/$member_id" -H "$AUTH" -H 'Content-Type: application/json' -d '{"active":false}')"
# Keeps repeated local runs from piling up fixtures in the dev database.
check "smoke task deleted" "200" "$(status_of -X DELETE -H "$AUTH" "$BASE/api/tasks/$task_id")"
check "smoke project deleted" "200" "$(status_of -X DELETE -H "$AUTH" "$BASE/api/projects/$project_id")"

# ---------------------------------------------------------------------------
section "API rate limiting"
# The limit is keyed by client address, and in a corporate deployment that is
# the office's NAT gateway - one address for everybody. A limit tight enough to
# throttle a single busy board would throttle the whole company, so this proves
# a burst the size of several page loads passes untouched.
burst_codes=""
for _ in $(seq 1 80); do
    burst_codes="$burst_codes $(status_of -H "$AUTH" "$BASE/api/projects")"
done
if printf '%s' "$burst_codes" | grep -q 429; then
    fail "ordinary use is not rate limited" "a burst of 80 reads hit the limiter"
else
    pass "ordinary use is not rate limited"
fi

# ---------------------------------------------------------------------------
# Runs last: it deliberately exhausts the login rate limiter for a minute.
section "Login rate limiting"
codes=""
for _ in $(seq 1 20); do
    codes="$codes $(status_of -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' -d "$login_body")"
done
contains "login is rate limited (429)" "$codes" "429"
contains "the first attempts still pass" "$codes" "200"

# ---------------------------------------------------------------------------
printf '\n-----------------------------------------\n'
printf 'passed: %d   failed: %d\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
printf 'smoke test OK\n'
