GO ?= go
GOLANGCI ?= golangci-lint
CLEANNACOS_TEST_ADDR ?=

# Local Nacos (docker-compose.yml). The gRPC port is derived by the Nacos SDK
# as HTTP port + 1000, so overriding NACOS_HTTP_PORT moves both mappings.
COMPOSE ?= docker compose
NACOS_HTTP_PORT ?= 8848
NACOS_GRPC_PORT ?= $(shell expr $(NACOS_HTTP_PORT) + 1000)
NACOS_ADDR ?= 127.0.0.1:$(NACOS_HTTP_PORT)

.PHONY: all test race vet lint fmt fmt-check integration-test tidy \
	nacos-up nacos-wait nacos-down integration-test-local example-e2e verify-local

all: fmt-check vet test

## test: run the unit tests
test:
	$(GO) test ./...

## race: run the unit tests with the race detector
race:
	$(GO) test -race ./...

## vet: run go vet
vet:
	$(GO) vet ./...

## lint: check formatting, vet and golangci-lint
lint: fmt-check vet
	$(GOLANGCI) run

## fmt-check: fail when files need gofmt
fmt-check:
	@files="$$(gofmt -l $$(find $$($(GO) list -f '{{.Dir}}' ./...) -maxdepth 1 -name '*.go'))"; \
	if [ -n "$$files" ]; then \
		echo "gofmt is required for:"; \
		echo "$$files"; \
		exit 1; \
	fi

## fmt: rewrite files with gofmt
fmt:
	gofmt -w .

## integration-test: run the tests against a real Nacos (set CLEANNACOS_TEST_ADDR)
## -race is intentionally left out: nacos-sdk-go v2.3.5 reports a race of its own
## in RpcClient.Shutdown when a config client is closed while listening.
## -count=1 keeps the result from being served from the Go test cache: the tests
## depend on an external Nacos, not only on the sources.
integration-test:
	CLEANNACOS_TEST_ADDR=$(CLEANNACOS_TEST_ADDR) $(GO) test -v -count=1 -run Integration ./...

## tidy: sync go.mod and go.sum
tidy:
	$(GO) mod tidy

## nacos-up: start the local Nacos used by the integration tests
nacos-up:
	NACOS_HTTP_PORT=$(NACOS_HTTP_PORT) NACOS_GRPC_PORT=$(NACOS_GRPC_PORT) $(COMPOSE) up -d

## nacos-wait: block until the local Nacos answers its readiness probe
nacos-wait:
	@for i in $$(seq 1 60); do \
		if curl -fsS "http://$(NACOS_ADDR)/nacos/v1/console/health/readiness" >/dev/null 2>&1; then \
			echo "nacos is ready at $(NACOS_ADDR)"; \
			exit 0; \
		fi; \
		echo "waiting for nacos at $(NACOS_ADDR) ($$i/60)"; \
		sleep 2; \
	done; \
	echo "nacos at $(NACOS_ADDR) did not become ready in time"; \
	exit 1

## nacos-down: stop the local Nacos and drop its data
nacos-down:
	NACOS_HTTP_PORT=$(NACOS_HTTP_PORT) NACOS_GRPC_PORT=$(NACOS_GRPC_PORT) $(COMPOSE) down -v

## integration-test-local: start a local Nacos, run the integration tests, always clean up
integration-test-local:
	@set -e; \
	trap '$(COMPOSE) down -v >/dev/null 2>&1 || true' EXIT; \
	$(MAKE) --no-print-directory nacos-up; \
	$(MAKE) --no-print-directory nacos-wait; \
	CLEANNACOS_TEST_ADDR=$(NACOS_ADDR) $(GO) test -v -count=1 -run Integration ./...

## example-e2e: publish the example configs and run both examples against Nacos
example-e2e:
	NACOS_ADDR=$(NACOS_ADDR) ./scripts/example-e2e.sh

## verify-local: start a local Nacos, run the integration tests and the examples, always clean up
verify-local:
	@set -e; \
	trap '$(COMPOSE) down -v >/dev/null 2>&1 || true' EXIT; \
	$(MAKE) --no-print-directory nacos-up; \
	$(MAKE) --no-print-directory nacos-wait; \
	CLEANNACOS_TEST_ADDR=$(NACOS_ADDR) $(GO) test -v -count=1 -run Integration ./...; \
	$(MAKE) --no-print-directory example-e2e
