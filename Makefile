.PHONY: help build install test clean

help:
	@echo "Dunbar - Build Commands"
	@echo ""
	@echo "  make build     - Build the CLI tool"
	@echo "  make install   - Install the CLI tool to /usr/local/bin"
	@echo "  make test      - Run tests"
	@echo "  make clean     - Remove built files"

build:
	@echo "Building CLI tool..."
	go build -o dunbar ./cmd/dunbar

install: build
	@echo "Installing CLI tool to /usr/local/bin..."
	sudo cp dunbar /usr/local/bin/dunbar

test:
	@echo "Running tests..."
	go test ./...

clean:
	@echo "Cleaning build artifacts..."
	rm -f dunbar
