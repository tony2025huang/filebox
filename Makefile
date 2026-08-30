.PHONY: dev build start build-linux release

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

# release 产出单文件交付物：Windows/Linux amd64 + SHA256 校验和（CGO_ENABLED=0、-trimpath、-s -w）。
# release produces the single-binary deliverables (Windows/Linux amd64) plus SHA256 checksums with CGO disabled and trimmed binaries.
release:
	npm --prefix web run build
	go run ./scripts/sync-web.go
	mkdir -p dist
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/filebox-windows-amd64.exe ./cmd/filebox
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/filebox-linux-amd64 ./cmd/filebox
	@echo "writing dist/SHA256SUMS.txt"
	@powershell -NoProfile -Command "$files = Get-ChildItem dist -File | Where-Object { $_.Name -ne 'SHA256SUMS.txt' }; $lines = foreach ($f in $files) { $h = (certutil -hashfile $f.FullName SHA256)[1] -replace ' ',''; $h.ToLowerInvariant() + '  ' + $f.Name }; Set-Content -Path dist/SHA256SUMS.txt -Value $lines -Encoding ascii"

start:
	go run ./cmd/filebox --addr=:8080 --data=./data
