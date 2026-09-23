.PHONY: web bin dist run test tidy

VERSION ?= 0.2.24
LDFLAGS := -s -w -X liking/internal/version.Version=$(VERSION)
export GOCACHE := $(CURDIR)/.gocache
export GOMODCACHE := $(CURDIR)/.gomod
export GOPATH := $(CURDIR)/.gopath

web:
	cd web && npm install --cache "$(CURDIR)/.npm-cache" && npm run build

bin: web
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/liking-server ./cmd/server
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/liking-agent ./cmd/agent

dist: bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/liking-agent-linux-amd64 ./cmd/agent
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/liking-agent-linux-arm64 ./cmd/agent
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/liking-server-linux-amd64 ./cmd/server
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/liking-server-linux-arm64 ./cmd/server

run: bin
	./bin/liking-server --addr 127.0.0.1:8899 --db ./data/panel.db

test:
	go test ./...
	cd web && node --test src/lib/dest.test.js

tidy:
	go mod tidy
