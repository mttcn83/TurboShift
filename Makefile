# TurboShift: small, dependency-light Go project. `make` builds ./turbo.
PREFIX ?= /usr/local
GO     ?= go

.PHONY: all build test vet fmt check distro-test notices release install uninstall clean

all: build

build:
	$(GO) build -trimpath -o turbo ./cmd/turbo

test:
	$(GO) test -race -count=1 ./...

vet:
	$(GO) vet ./...

fmt:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)

# Everything CI runs.
check: fmt vet test

# Disposable-container install + smoke + unit tests on five distributions.
distro-test:
	scripts/distro-test.sh

# Regenerate THIRD_PARTY_NOTICES.md from the modules actually linked in.
notices:
	scripts/gen-notices.sh

# Static, stripped release binaries and checksums in ./dist.
release:
	mkdir -p dist
	for a in amd64 arm64; do CGO_ENABLED=0 GOOS=linux GOARCH=$$a $(GO) build -trimpath -ldflags "-s -w" -o dist/turbo-linux-$$a ./cmd/turbo; done
	cd dist && sha256sum turbo-linux-* > SHA256SUMS

install: build
	install -Dm755 turbo $(DESTDIR)$(PREFIX)/bin/turbo

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/turbo

clean:
	rm -f turbo
