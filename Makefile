APP := portguard
GOFLAGS := CGO_ENABLED=0
LDFLAGS := -s -w -X main.version=$(VERSION)
VERSION ?= 2.12.0

.PHONY: all web server linux test clean

all: web linux

## build the React dashboard
web:
	cd web && npm ci --no-fund --no-audit && npm run build

## build the server binary for the host OS
server:
	$(GOFLAGS) go build -ldflags "$(LDFLAGS)" -o bin/portguard ./cmd/server

## build the Linux amd64 release binary
linux:
	$(GOFLAGS) GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/portguard-linux-amd64 ./cmd/server

test:
	go test ./...

clean:
	rm -rf bin
