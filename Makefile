# wpcalc — build and verification targets.
#
# CGO_ENABLED=0 is not optional: the whole dual-use design rests on shipping one
# static binary that a PHP shim can spawn on a host we do not control.
# GOPRIVATE is still required after moving to GitHub: the repository is
# private, so the public module proxy and checksum database cannot fetch it
# and a lookup there would fail rather than fall through.

export CGO_ENABLED = 0
export GOPRIVATE = github.com/rolsim/*

BIN := bin/wpcalc
PKGS := ./...

# Stamped only for tagged builds. Untagged ones need nothing: the toolchain
# embeds the revision, commit time and dirty flag, and the binary derives its
# own identifier from those. Overriding with a bare short SHA here would just
# restate what is already inside, less precisely.
VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null || git describe --tags 2>/dev/null)
ifneq ($(VERSION),)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"
endif

.PHONY: all build vet lint test check e2e e2e-wp fmt tidy clean

all: check

build:
	go build $(LDFLAGS) -o $(BIN) ./cmd/wpcalc

vet:
	go vet $(PKGS)

lint:
	golangci-lint run

test:
	go test $(PKGS)

# The gate every commit must pass.
check: build vet lint test

# Browser e2e against the headless-shell container.
e2e:
	go test -tags e2e -count=1 ./e2e/standalone/...

# WordPress e2e: docker compose stack + wp-cli + chromedp. Slow, tagged out of `test`.
e2e-wp:
	go test -tags e2e_wp -count=1 -timeout 20m ./e2e/wordpress/...

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

clean:
	rm -rf bin
	docker compose -p wpcalc-e2e down -v 2>/dev/null || true
