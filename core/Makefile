VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test vet dist docker
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/yimeng-core ./cmd/core
test:
	go test ./...
vet:
	go vet ./...
# Static binaries for NAS without Docker.
dist:
	for a in amd64 arm64; do CGO_ENABLED=0 GOOS=linux GOARCH=$$a go build -trimpath -ldflags="$(LDFLAGS)" -o dist/yimeng-core-linux-$$a ./cmd/core; done
docker:
	docker buildx build --platform linux/amd64,linux/arm64 -t yimeng-core:$(VERSION) .
