.PHONY: build build-with-version test clean

build:
	go build -ldflags "\
		-X 'main.Version=v1.0.0' \
		-X 'main.Commit=$$(git rev-parse HEAD)' \
		-X 'main.BuildTime=$$(date -u +%Y-%m-%dT%H:%M:%SZ)'" \
		-o claude cmd/claude/main.go

# Run tests
test:
	go test ./cmd/claude ./pkg/...

# Clean build artifacts
clean:
	rm -f claude main
	go clean

# Install to $GOPATH/bin
install:
	go install -ldflags "\
		-X 'main.Version=v1.0.0' \
		-X 'main.Commit=$$(git rev-parse HEAD)' \
		-X 'main.BuildTime=$$(date -u +%Y-%m-%dT%H:%M:%SZ)'" \
		./cmd/claude
