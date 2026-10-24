VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64
SHA256 := $(shell command -v shasum >/dev/null 2>&1 && echo "shasum -a 256" || echo sha256sum)

.PHONY: run test test-postgres build dist lint docker clean

run:
	go run ./cmd/greenroom

test:
	go test -timeout 90s ./...

test-postgres:
	@test -n "$(GREENROOM_TEST_POSTGRES_URL)" || { echo "GREENROOM_TEST_POSTGRES_URL is not set" >&2; exit 1; }
	GREENROOM_TEST_POSTGRES_URL="$(GREENROOM_TEST_POSTGRES_URL)" go test -timeout 90s ./...

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/greenroom ./cmd/greenroom

dist:
	rm -rf dist
	mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		out="dist/greenroom_$(VERSION)_$${os}_$${arch}$${ext}"; \
		echo "$$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o "$$out" ./cmd/greenroom || exit 1; \
	done
	cd dist && $(SHA256) greenroom_* > checksums.txt

lint:
	go vet ./...
	test -z "$$(gofmt -l .)"
	sh scripts/no-comments.sh

docker:
	docker build -t greenroom:$(VERSION) --build-arg VERSION=$(VERSION) .

clean:
	rm -rf bin dist
