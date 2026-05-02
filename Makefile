.PHONY: help preflight-vendor prepare docker-up docker-down docker-logs unit-test integration-test build

GENERATED = .generated
DOCKER_COMPOSE = docker compose -f $(GENERATED)/docker-compose.yml --env-file $(GENERATED)/.env
DELIVERYDRON_ROOT ?= systems/deliverydron

help:
	@echo "make prepare     - Generate docker-compose + .env from broker and components"
	@echo "make preflight-vendor - Verify vendor/ exists (run 'go mod vendor' from repo root if missing)"
	@echo "make docker-up   - Start system (prepare + docker compose up)"
	@echo "make docker-down - Stop system"
	@echo "make docker-logs - Follow logs"
	@echo "make unit-test   - Run Go unit tests for all components"
	@echo "make integration-test - Run in-process integration tests (TestIntegration_*)"

preflight-vendor:
	@cd ../.. && test -d vendor || (echo "vendor/ not found. Run: go mod vendor (from repo root)"; exit 1)

prepare: preflight-vendor
	@cd ../.. && python3 scripts/prepare_system.py $(DELIVERYDRON_ROOT)

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

# Integration tests from tests/integration_*_test.go (memory bus + components; see tests/README.md).
integration-test:
	@test -d vendor || (echo "vendor/ not found. Run: go mod vendor (from module root)"; exit 1)
	@go test -mod=vendor -v -count=1 ./tests -run '^TestIntegration_'

build:
	@cd ../.. && go build -o /dev/null ./$(DELIVERYDRON_ROOT)/delivery_drone/cmd/delivery_drone ./$(DELIVERYDRON_ROOT)/stub_component/cmd/stub_component
