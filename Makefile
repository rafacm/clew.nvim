BIN := bin/clew
PKG := ./cmd/clew

.PHONY: all build test test-go test-lua test-acceptance fmt vet clean tidy docs

all: build

build:
	@mkdir -p bin
	go build -o $(BIN) $(PKG)
	@echo "built $(BIN)"

# The default run is hermetic: no network, no toolchain, no indexer. See
# doc/adr/0001-testing-strategy.md. Tier 3 lives behind `make test-acceptance`.
test: test-go test-lua

test-go:
	go test -race ./...

# plenary is cloned into .tests/ on first run. Set CLEW_PLENARY_DIR to reuse a
# checkout you already have.
test-lua:
	nvim --headless --noplugin -u tests/minimal_init.lua \
		-c "PlenaryBustedDirectory tests/ { minimal_init = 'tests/minimal_init.lua' }"

# Tier 3. Downloads real projects at pinned commits and drives the real
# indexers: needs the network, a JDK, Maven and Node. Never run by `make test`.
test-acceptance:
	go test -tags acceptance -timeout 60m -v ./internal/acceptance/

fmt:
	go fmt ./...

vet:
	go vet ./...

tidy:
	go mod tidy

# Regenerate doc/tags so :help clew works from a source checkout. Plugin
# managers run this on install; doing it here catches a malformed *tag* before
# a user does.
docs:
	nvim --headless -c 'helptags doc' -c q
	@echo "wrote doc/tags"

clean:
	rm -rf bin
