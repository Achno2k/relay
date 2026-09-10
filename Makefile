BINARY := agents
PKG := github.com/Achno2k/agents-cli
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(PKG)/internal/cli.Version=$(VERSION) -X $(PKG)/internal/bootstrap.BuildSourceDir=$(CURDIR)

.PHONY: build install test lint snapshot clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/agents

# rm first: overwriting an existing Mach-O in place leaves macOS with a stale
# cached code signature and the next run dies with SIGKILL (exit 137).
install: build
	rm -f $(HOME)/.local/bin/$(BINARY)
	cp bin/$(BINARY) $(HOME)/.local/bin/$(BINARY)

test:
	go test ./...

# lint runs go vet and enforces gofmt formatting.
lint:
	go vet ./...
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then \
		echo "gofmt needed on:"; echo "$$out"; exit 1; fi

snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf bin dist
