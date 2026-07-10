export PATH := $(HOME)/.local/go/bin:$(PATH)

VERSION ?= $(shell tr -d '[:space:]' < VERSION)
LDFLAGS := -s -w -X main.version=$(VERSION)
SRC := ./cmd/pastel

.PHONY: build check test cross clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/pastel $(SRC)

test:
	go test ./...

check:
	test -z "$$(gofmt -l .)"
	go mod verify
	go vet ./...
	go test -race ./...

# Build the same target matrix published by GitHub Releases.
cross: build
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/pastel-darwin-arm64  $(SRC)
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/pastel-darwin-amd64  $(SRC)
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/pastel-linux-amd64   $(SRC)
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/pastel-linux-arm64   $(SRC)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/pastel-windows-amd64.exe $(SRC)
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/pastel-windows-arm64.exe $(SRC)
	@echo ""
	@ls -lh bin/

clean:
	rm -rf bin
