.PHONY: build test unit-test docker-build docker-up run vendor deps

BINARY := delivery-drone
DOCKER_IMAGE := delivery_drone
# Use vendored deps if present (avoids TLS timeouts / network at build time)
BUILD_MOD := $(if $(wildcard vendor),-mod=vendor,)

build:
	GOPROXY=DIRECT CGO_ENABLED=0 go build $(BUILD_MOD) -o $(BINARY) ./cmd/delivery_drone

# Download deps (use if go build fails with TLS handshake timeout)
deps:
	go mod download

# Vendor deps so build works offline; run once when network is OK (e.g. after deps)
vendor:
	go mod vendor

test: unit-test

unit-test:
	GOPROXY=DIRECT CGO_ENABLED=0 go test ./... -v

# Requires vendored deps: run 'make vendor' first if vendor/ is missing.
# Prefer buildx (BuildKit) to avoid "legacy builder deprecated" warning; falls back to docker build.
docker-build:
	@if docker buildx version >/dev/null 2>&1; then \
		docker buildx build --load -t $(DOCKER_IMAGE) -f docker/Dockerfile .; \
	else \
		docker build -t $(DOCKER_IMAGE) -f docker/Dockerfile .; \
	fi

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
