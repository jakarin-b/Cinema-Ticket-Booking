#!/bin/sh
set -eu

docker compose down -v --remove-orphans
docker compose up --build -d --wait --wait-timeout 180
docker compose ps
curl --fail --silent http://localhost:8080/health/live >/dev/null
curl --fail --silent http://localhost:9091/health/live >/dev/null
curl --fail --silent http://localhost:3000/ >/dev/null
curl --fail --silent http://localhost:8080/api/v1/movies >/dev/null
curl --fail --silent http://localhost:8081/ >/dev/null
curl --fail --silent http://localhost:9090/-/healthy >/dev/null
curl --fail --silent http://localhost:16686/ >/dev/null
curl --fail --silent http://localhost:8080/metrics >/dev/null
curl --fail --silent http://localhost:9091/metrics >/dev/null
node ./scripts/websocket-smoke.mjs
docker compose --profile test run --rm concurrency-test
echo "Clean startup and acceptance verification passed."
