BIN := bin/clew
PKG := ./cmd/clew

.PHONY: all build test fmt vet clean tidy docs

all: build

build:
	@mkdir -p bin
	go build -o $(BIN) $(PKG)
	@echo "built $(BIN)"

test:
	go test ./...

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
