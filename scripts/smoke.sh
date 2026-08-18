#!/usr/bin/env bash
set -euo pipefail
base=${BASE_URL:-http://localhost:3001}
user=${MINIUPTIME_USER:-admin123}
pass=${MINIUPTIME_PASSWORD:-admin1234567}
cookie=$(mktemp); trap 'rm -f "$cookie"' EXIT
get(){ curl --fail-with-body --silent --show-error -b "$cookie" -c "$cookie" "$base$1"; }
code(){ curl --silent --show-error -o /dev/null -w '%{http_code}' -b "$cookie" -c "$cookie" "$base$1" "${@:2}"; }
login=$(get /login)
csrf=$(printf '%s' "$login" | sed -n 's/.*name="csrf" value="\([^"]*\)".*/\1/p')
test -n "$csrf"
[ "$(code /login -X POST)" = 403 ]
[ "$(code /login -X POST --data-urlencode "csrf=$csrf" --data-urlencode "username=$user" --data-urlencode "password=$pass")" = 303 ]
for path in /dashboard /monitors /groups /incidents /settings /monitors/new; do [ "$(code "$path")" = 200 ]; done
[ "$(code /monitors -X POST --data-urlencode "csrf=$csrf" --data-urlencode type=http --data-urlencode name=smoke-invalid --data-urlencode target=bad --data-urlencode interval=60)" = 400 ]
events=$(timeout 8 curl --silent --show-error -N -b "$cookie" "$base/events" || true); grep -q 'event: status' <<<"$events"
printf '%s\n' 'smoke: PASS'
