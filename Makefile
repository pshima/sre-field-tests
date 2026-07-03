# SRE Field Tests — build & test.
#
# All binaries are built CGO-free and statically linked so they run anywhere,
# including the scratch/distroless containers the observer ships in.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GO      := CGO_ENABLED=0 go
BIN     := bin

.PHONY: all build sreft observer test vet fmt tidy clean cross

all: build

build: sreft observer

sreft:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN)/sreft ./cmd/sreft

observer:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN)/observer ./cmd/observer

# Cross-compile the observer for the Linux targets scenarios run on.
cross:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/observer-linux-amd64 ./cmd/observer
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/observer-linux-arm64 ./cmd/observer

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w -s .

tidy:
	go mod tidy

clean:
	rm -rf $(BIN)
