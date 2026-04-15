.PHONY: help prepare docker-up docker-down docker-logs unit-test build run run-down run-logs

GENERATED = .generated
DOCKER_COMPOSE = docker compose -f $(GENERATED)/docker-compose.yml --env-file $(GENERATED)/.env

help:
	@echo "make prepare     - Generate docker-compose + .env from broker and components"
	@echo "make docker-up   - Start system (prepare + docker compose up)"
	@echo "make docker-down - Stop system"
	@echo "make docker-logs - Follow logs"
	@echo "make run         - Robust startup (go mod download/vendor/build + up)"
	@echo "make run-down    - Stop stack used by run script"
	@echo "make run-logs    - Follow logs used by run script"
	@echo "make unit-test   - Run Go unit tests for all components"

prepare:
	@cd ../.. && python3 scripts/prepare_system.py systems/deliverydron

docker-up: prepare
	@set -a && . $(GENERATED)/.env && set +a && \
		$(DOCKER_COMPOSE) --profile $${BROKER_TYPE:-kafka} up -d --build

docker-down:
	-$(DOCKER_COMPOSE) --profile kafka down 2>/dev/null
	-$(DOCKER_COMPOSE) --profile mqtt down 2>/dev/null

docker-logs:
	@set -a && . $(GENERATED)/.env && set +a && \
		$(DOCKER_COMPOSE) --profile $${BROKER_TYPE:-kafka} logs -f

unit-test:
	@cd ../.. && go test ./... -v -count=1

build:
	@cd ../.. && go build -o /dev/null ./cmd/delivery_drone ./cmd/stub_component

run:
	@./run-system.sh up

run-down:
	@./run-system.sh down

run-logs:
	@./run-system.sh logs
