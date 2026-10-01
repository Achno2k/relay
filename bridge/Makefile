BINARY := relay
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X relay/internal/api.Version=$(VERSION) -X relay/internal/bootstrap.BuildSourceDir=$(CURDIR)

.PHONY: build install test lint clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/relay

# rm first: overwriting an existing Mach-O in place leaves macOS with a stale
# cached code signature and the next run dies with SIGKILL (exit 137).
install: build
	rm -f $(HOME)/.local/bin/$(BINARY)
	cp bin/$(BINARY) $(HOME)/.local/bin/$(BINARY)

test:
	go test -race ./...

# lint runs go vet for both targets and enforces gofmt formatting.
lint:
	go vet ./...
	GOOS=linux go vet ./...
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then \
		echo "gofmt needed on:"; echo "$$out"; exit 1; fi

clean:
	rm -rf bin
