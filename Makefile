.PHONY: build test test-race fmt vet check run tidy clean release release-checksums

VERSION ?= dev
LDFLAGS := -s -w

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/codebuddy-proxy ./cmd/codebuddy-proxy

test:
	go test ./...

test-race:
	go test -race ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './bin/*')

vet:
	go vet ./...

check: fmt vet test-race

run:
	go run ./cmd/codebuddy-proxy

tidy:
	go mod tidy

clean:
	rm -rf bin releases

release:
	@mkdir -p releases
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o releases/codebuddy-proxy-windows-x64.exe ./cmd/codebuddy-proxy
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o releases/codebuddy-proxy-linux-amd64 ./cmd/codebuddy-proxy
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o releases/codebuddy-proxy-darwin-arm64 ./cmd/codebuddy-proxy
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o releases/codebuddy-proxy-darwin-amd64 ./cmd/codebuddy-proxy
	cp .env.example releases/.env.example
	$(MAKE) release-checksums

release-checksums:
	@cd releases && (sha256sum codebuddy-proxy-* 2>/dev/null || shasum -a 256 codebuddy-proxy-*) > SHA256SUMS.txt
