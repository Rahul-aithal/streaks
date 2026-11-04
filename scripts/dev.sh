#!/usr/bin/env bash
# Runs the API and the web dev server; stopping one stops the other.
set -u
[ -d node_modules ] || pnpm install

./bin/streaks serve &
api=$!
(cd apps/web && exec ./node_modules/.bin/vite dev) &
web=$!

cleanup() {
  trap - INT TERM EXIT
  kill "$api" "$web" 2>/dev/null
  wait "$api" "$web" 2>/dev/null
}
trap cleanup INT TERM EXIT
wait -n "$api" "$web"
