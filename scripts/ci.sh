#!/bin/bash

set -e

echo "Running CI checks for livecore..."

# Check if we're in the right directory
if [ ! -f "go.mod" ]; then
    echo "Error: go.mod not found. Please run from the project root."
    exit 1
fi

# Set environment variables
export GOOS=linux
export GOARCH=amd64
export CGO_ENABLED=0

echo "Environment:"
echo "  GOOS=$GOOS"
echo "  GOARCH=$GOARCH"
echo "  CGO_ENABLED=$CGO_ENABLED"

# Go vet
echo "Running go vet..."
go vet ./...

# Go fmt check
echo "Checking go fmt..."
if [ -n "$(gofmt -s -l .)" ]; then
    echo "Error: Code is not formatted with go fmt"
    gofmt -s -l .
    exit 1
fi

# Go mod tidy
echo "Running go mod tidy..."
go mod tidy

# Check for go.mod changes (skip if git not available or not a git repo)
if command -v git >/dev/null 2>&1 && git rev-parse --git-dir >/dev/null 2>&1; then
    if [ -n "$(git diff go.mod go.sum)" ]; then
        echo "Error: go.mod or go.sum has uncommitted changes"
        git diff go.mod go.sum
        exit 1
    fi
else
    echo "Warning: git not available or not a git repository, skipping go.mod change check"
fi

# Go test
echo "Running go test..."
go test -v ./...

# Go test with race detector (only if CGO is enabled)
if [ "$CGO_ENABLED" = "1" ]; then
    echo "Running go test with race detector..."
    go test -race ./...
else
    echo "Skipping race detector test (CGO disabled)"
fi

# Build
echo "Building livecore..."
go build -o livecore ./cmd/livecore

# Check if binary was created
if [ ! -f "livecore" ]; then
    echo "Error: Binary not created"
    exit 1
fi

# Check binary
echo "Checking binary..."
if command -v file >/dev/null 2>&1; then
    file livecore
else
    echo "Warning: file command not available, skipping binary check"
fi
ls -la livecore

# Test help
echo "Testing help output..."
./livecore -h

echo "All CI checks passed!"
