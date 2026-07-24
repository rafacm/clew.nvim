BIN := bin/clew
PKG := ./cmd/clew

.PHONY: all build test fmt vet clean tidy

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

clean:
	rm -rf bin
