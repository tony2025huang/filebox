.PHONY: dev build start build-linux

dev:
	go run ./cmd/filebox

build:
	npm --prefix web run build
	go run ./scripts/sync-web.go
	mkdir -p bin
	go build -o bin/filebox ./cmd/filebox

build-linux:
	npm --prefix web run build
	go run ./scripts/sync-web.go
	mkdir -p bin
	set GOOS=linux&& set GOARCH=amd64&& go build -o bin/filebox-linux ./cmd/filebox

start:
	go run ./cmd/filebox --addr=:8080 --data=./data
