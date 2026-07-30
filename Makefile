# wpcalc — build and verification targets.
#
# CGO_ENABLED=0 is not optional: the whole dual-use design rests on shipping one
# static binary that a PHP shim can spawn on a host we do not control.
# GOPRIVATE keeps the module proxy and checksum database out of resolving
# this module's own path, regardless of the repo's current visibility.

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

.PHONY: all build vet lint test check sdk-check ctl-build ctl-check check-all e2e e2e-wp fmt tidy clean

all: check

build:
	go build $(LDFLAGS) -o $(BIN) ./cmd/wpcalc

vet:
	go vet $(PKGS)

lint:
	golangci-lint run

test:
	go test $(PKGS)

# The gate every commit must pass. Server only — sdk/go and cmd/wpcalcctl
# are separate Go modules by design (so consuming either doesn't pull in
# the server's own dependencies) and so aren't covered here.
check: build vet lint test

sdk-check:
	cd sdk/go && go build ./... && go vet ./... && golangci-lint run && go test ./...

ctl-build:
	cd cmd/wpcalcctl && go build -o ../../bin/wpcalcctl .

ctl-check: ctl-build
	cd cmd/wpcalcctl && go vet ./... && golangci-lint run && go test ./...

# Every module's own gate, in one command.
check-all: check sdk-check ctl-check

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
