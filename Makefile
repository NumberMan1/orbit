GO ?= go
GOFMT ?= $(shell $(GO) env GOROOT)/bin/gofmt
export GOTOOLCHAIN := local

.PHONY: check-version fmt vet test race fuzz build verify integration generated
check-version:
	@test "$$($(GO) env GOVERSION)" = "go1.27.2" || (echo "Go 1.27.2 is required"; exit 1)
fmt: check-version
	@unformatted="$$($(GOFMT) -l .)" && test -z "$$unformatted"
vet: check-version
	$(GO) vet ./...
test: check-version
	$(GO) test -timeout=60s ./...
race: check-version
	$(GO) test -race -timeout=90s ./...
fuzz: check-version
	$(GO) test ./rpc -run='^$$' -fuzz=FuzzRead -fuzztime=10s
	$(GO) test ./request -run='^$$' -fuzz=FuzzMetadata -fuzztime=10s
build: check-version
	$(GO) build ./...
generated: check-version
	$(GO) run ./cmd/orbitgen -in examples/gateway/protocol/messages.json -out examples/gateway/protocol/bindings_gen.go -check
verify: fmt vet race build generated
integration: check-version
	$(GO) test -race -tags=integration -timeout=90s ./discovery ./storage/...
