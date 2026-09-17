GO ?= go
GOLANGCI ?= golangci-lint
CLEANNACOS_TEST_ADDR ?=

.PHONY: all test race vet lint fmt fmt-check integration-test tidy

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
integration-test:
	CLEANNACOS_TEST_ADDR=$(CLEANNACOS_TEST_ADDR) $(GO) test -v -run Integration ./...

## tidy: sync go.mod and go.sum
tidy:
	$(GO) mod tidy
