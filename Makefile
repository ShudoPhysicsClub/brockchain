.PHONY: build build-server build-client build-client-go build-client-ts clean

# Build all
build: build-server build-client

# Build server
build-server:
	cd server && go build -o brockchain .

# Build client (everything)
build-client: build-client-go build-client-ts

# Build Go client
build-client-go:
	cd client/go && go build -o brockchain-client-go .

# Build TypeScript client
build-client-ts:
	cd client/ts && npm install && npm run build

# Cross-platform builds
build-server-all:
	cd server && chmod +x build.sh && ./build.sh

build-client-go-all:
	cd client/go && chmod +x build.sh && ./build.sh

# Clean
clean:
	rm -rf server/brockchain server/dist \
	       client/go/brockchain-client-* client/go/dist \
	       client/ts/lib

# Development
run-server:
	cd server && go run .

run-client-go:
	cd client/go && go run . --help

dev-watch-server:
	cd server && go run .

help:
	@echo "Available targets:"
	@echo "  make build              - Build server and client (current platform)"
	@echo "  make build-server       - Build server only"
	@echo "  make build-client       - Build both Go and TypeScript clients"
	@echo "  make build-client-go    - Build Go client"
	@echo "  make build-client-ts    - Build TypeScript client"
	@echo "  make build-server-all   - Cross-platform server builds"
	@echo "  make build-client-go-all - Cross-platform Go client builds"
	@echo "  make clean              - Clean build artifacts"
	@echo "  make run-server         - Run server"
	@echo "  make run-client-go      - Run Go client"
	@echo "  make help               - Show this help"
