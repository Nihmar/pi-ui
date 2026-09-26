# pi-ui server (Phase 1 spike).
#
# check   what CI and AGENTS.md require: gofmt clean, vet, race tests, static build
# build   static binary in bin/pi-ui with the version and commit stamped in
# gen     regenerate internal/protocol/gen from ../schemas (idempotent)
# drift   fail when regenerating changes the committed generated files
# test    race tests only
# clean   drop build outputs
#
# Version stamping is overridable: make build VERSION=0.1.0 COMMIT=$(git rev-parse --short HEAD)

VERSION ?= 0.0.1-spike
COMMIT  ?= none
GO      ?= go
LDFLAGS  = -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: check build gen drift test clean

check:
	@test -z "$$(gofmt -l .)" || { echo "gofmt -l . reported unformatted files:"; gofmt -l .; exit 1; }
	$(GO) vet ./...
	$(GO) test -race ./...
	CGO_ENABLED=0 $(GO) build ./...

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/pi-ui ./cmd/pi-ui

gen:
	bash scripts/gen.sh

drift:
	bash scripts/gen.sh
	git diff --exit-code -- internal/protocol/gen

test:
	$(GO) test -race ./...

clean:
	rm -rf bin
