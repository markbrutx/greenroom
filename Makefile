VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64
SHA256 := $(shell command -v shasum >/dev/null 2>&1 && echo "shasum -a 256" || echo sha256sum)

.PHONY: run test build dist lint docker clean

run:
	go run ./cmd/moored

test:
	go test -timeout 90s ./...

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/moored ./cmd/moored

dist:
	rm -rf dist
	mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		out="dist/moored_$(VERSION)_$${os}_$${arch}$${ext}"; \
		echo "$$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o "$$out" ./cmd/moored || exit 1; \
	done
	cd dist && $(SHA256) moored_* > checksums.txt

lint:
	go vet ./...
	test -z "$$(gofmt -l .)"
	sh scripts/no-comments.sh

docker:
	docker build -t moored:$(VERSION) --build-arg VERSION=$(VERSION) .

clean:
	rm -rf bin dist
