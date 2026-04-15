.PHONY: build test unit-test test-e2e docker-build docker-test docker-up run vendor deps init prepare system-up system-down build-all

BINARY := delivery-drone
DOCKER_IMAGE := delivery_drone
# Use vendored deps if present (avoids TLS timeouts / network at build time)
BUILD_MOD := $(if $(wildcard vendor),-mod=vendor,)

# Copy broker env so prepare_system.py can merge it (optional; script falls back to example.env)
init:
	@test -f docker/.env || (cp docker/example.env docker/.env && echo "Created docker/.env from example.env")

# Generate systems/deliverydron/.generated/docker-compose.yml and .env (requires PyYAML: pip install pyyaml)
prepare: init
	python3 scripts/prepare_system.py systems/deliverydron

# Start full deliverydron system (broker + all components). Run 'make vendor' first for offline Docker build.
system-up:
	@cd systems/deliverydron && $(MAKE) docker-up

system-down:
	@cd systems/deliverydron && $(MAKE) docker-down

build:
	GOPROXY=DIRECT CGO_ENABLED=0 go build $(BUILD_MOD) -o $(BINARY) ./components/delivery_drone/cmd/delivery_drone

# Download deps (use if go build fails with TLS handshake timeout)
deps:
	go mod download

# Vendor deps so build works offline; run once when network is OK (e.g. after deps)
vendor:
	go mod vendor

test: unit-test

unit-test:
	GOPROXY=DIRECT CGO_ENABLED=0 go test ./... -v -cover

# Real broker: E2E_KAFKA=1 KAFKA_BOOTSTRAP_SERVERS=host:port BROKER_USER=... BROKER_PASSWORD=...
test-e2e:
	GOPROXY=DIRECT CGO_ENABLED=0 go test -tags=e2e ./tests/e2e/... -v

# Requires vendored deps: run 'make vendor' first if vendor/ is missing.
# Prefer buildx (BuildKit) to avoid "legacy builder deprecated" warning; falls back to docker build.
docker-build:
	@if docker buildx version >/dev/null 2>&1; then \
		docker buildx build --load -t $(DOCKER_IMAGE) -f docker/Dockerfile .; \
	else \
		docker build -t $(DOCKER_IMAGE) -f docker/Dockerfile .; \
	fi

docker-test: docker-build
	docker run --rm --entrypoint /bin/sh $(DOCKER_IMAGE) -c "test -x /app/delivery-drone"


# Expects broker on drones_net. Start parent first:
#   cd /path/to/sbd-drones-economics && docker compose -f docker/docker-compose.yml --env-file docker/.env --profile kafka up -d
# Broker auth (parent Kafka uses SASL): pass BROKER_USER/BROKER_PASSWORD; defaults match parent docker/example.env. For parent .env use: BROKER_USER=admin BROKER_PASSWORD=<from docker/.env> make docker-up
BROKER_USER ?= admin
BROKER_PASSWORD ?= admin_secret_123
docker-up:
	docker run --rm --name $(DOCKER_IMAGE) \
		--network drones_net \
		-e COMPONENT_ID=delivery_drone \
		-e BROKER_TYPE=kafka \
		-e KAFKA_BOOTSTRAP_SERVERS=kafka:29092 \
		-e BROKER_USER=$(BROKER_USER) \
		-e BROKER_PASSWORD=$(BROKER_PASSWORD) \
		-e HEALTH_PORT=8080 \
		-p 8080:8080 \
		$(DOCKER_IMAGE)

run: build
	./$(BINARY)

# Build both delivery_drone and stub_component (used by system Dockerfiles)
build-all:
	GOPROXY=DIRECT CGO_ENABLED=0 go build $(BUILD_MOD) -o /dev/null ./components/delivery_drone/cmd/delivery_drone ./components/stub_component/cmd/stub_component
