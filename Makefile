BINARY := aibench
BIN_DIR := bin

.PHONY: all build run tidy fmt vet lint test schema tui-shot tui-gif acc-test clean

all: build run

run: build
	./$(BIN_DIR)/$(BINARY)

build:
	go build -o $(BIN_DIR)/$(BINARY) .

tidy:
	go mod tidy

fmt:
	gofmt -w .
	golangci-lint fmt ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

test:
	go test -race ./...

# schema/aibench.schema.json is the copy SchemaURL serves raw off main;
# internal/config/schema.json is the one the binary embeds. Run this after
# editing the embedded one — a test fails if the two drift apart.
schema: build
	./$(BIN_DIR)/$(BINARY) schema > schema/aibench.schema.json

tui-shot: build
	./scripts/tui-shot.sh

tui-gif: build
	./scripts/tui-gif.sh

# Hits the LIVE endpoints with real credentials — developer-run only.
acc-test: build
	./scripts/acc-test.sh

clean:
	rm -rf $(BIN_DIR)
