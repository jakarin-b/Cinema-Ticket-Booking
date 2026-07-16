#!/bin/sh
set -eu

docker compose up --build -d --remove-orphans --wait --wait-timeout 180
docker compose ps
curl --fail --silent http://localhost:8080/health/live >/dev/null
curl --fail --silent http://localhost:9091/health/live >/dev/null
curl --fail --silent http://localhost:8025/ >/dev/null
curl --fail --silent http://localhost:3000/ >/dev/null
curl --fail --silent http://localhost:8080/api/v1/movies >/dev/null
curl --fail --silent http://localhost:8081/ >/dev/null
node ./scripts/websocket-smoke.mjs
docker compose --profile test run --rm concurrency-test
echo "Non-destructive startup and acceptance verification passed."
