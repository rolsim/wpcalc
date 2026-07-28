# wpcalc — build and verification targets.
#
# CGO_ENABLED=0 is not optional: the whole dual-use design rests on shipping one
# static binary that a PHP shim can spawn on a host we do not control.
# GOPRIVATE keeps the internal module path away from the public proxy/sumdb.

export CGO_ENABLED = 0
export GOPRIVATE = source.simonet.internal/*

BIN := bin/wpcalc
PKGS := ./...

.PHONY: all build vet lint test check e2e e2e-wp fmt tidy clean

all: check

build:
	go build -o $(BIN) ./cmd/wpcalc

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
