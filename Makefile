# TurboShift: small, dependency-light Go project. `make` builds ./turbo.
PREFIX ?= /usr/local
POLKIT_DIR ?= /usr/share/polkit-1/actions
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

# The polkit action makes one password prompt cover a whole preset (see
# packaging/polkit). Without it everything still works, with a prompt per step.
install: build
	install -Dm755 turbo $(DESTDIR)$(PREFIX)/bin/turbo
	install -d $(DESTDIR)$(POLKIT_DIR)
	sed 's|@BINARY@|$(PREFIX)/bin/turbo|' packaging/polkit/io.github.mttcn83.turboshift.policy.in > $(DESTDIR)$(POLKIT_DIR)/io.github.mttcn83.turboshift.policy
	chmod 644 $(DESTDIR)$(POLKIT_DIR)/io.github.mttcn83.turboshift.policy

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/turbo $(DESTDIR)$(POLKIT_DIR)/io.github.mttcn83.turboshift.policy

clean:
	rm -f turbo
