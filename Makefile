.PHONY: up down logs seed test test-backend test-frontend test-concurrency test-websocket verify

up:
	docker compose up --build

down:
	docker compose down -v --remove-orphans

logs:
	docker compose logs -f api worker

seed:
	docker compose run --rm seed

test: test-backend test-frontend

test-backend:
	cd backend && go test ./...

test-frontend:
	cd frontend && npm test

test-concurrency:
	docker compose --profile test run --rm concurrency-test

test-websocket:
	node ./scripts/websocket-smoke.mjs

verify:
	sh ./scripts/verify.sh
