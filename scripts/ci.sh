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
go build -o livecore .

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

# Test with HTTP server
echo "Testing with HTTP server..."
cd test/httpserver

# Build the HTTP server
echo "Building HTTP server test program..."
go build -o httpserver .

# Find an available port
PORT=8081
while netstat -ln 2>/dev/null | grep -q ":$PORT "; do
    PORT=$((PORT + 1))
done

# Start the HTTP server in the background
echo "Starting HTTP server on port $PORT..."
./httpserver $PORT &
SERVER_PID=$!

# Wait for server to start with proper polling
echo "Waiting for server to start..."
for i in {1..30}; do
    if curl -s http://127.0.0.1:$PORT/status >/dev/null 2>&1; then
        echo "HTTP server is responsive"
        break
    fi
    if [ $i -eq 30 ]; then
        echo "Error: HTTP server not responding after 30 attempts"
        kill $SERVER_PID 2>/dev/null || true
        exit 1
    fi
    sleep 0.1
done

# Run livecore against the HTTP server
echo "Running livecore against HTTP server..."
cd ../..

# Check if server is still running
if ! kill -0 $SERVER_PID 2>/dev/null; then
    echo "Error: HTTP server process died before core dump"
    exit 1
fi

# Run livecore - this MUST succeed for CI to pass
echo "Running livecore against HTTP server..."
if ./livecore -verbose -passes 2 -dirty-thresh 10 $SERVER_PID test_httpserver.core; then
    echo "✅ livecore completed successfully"
    CORE_DUMP_SUCCESS=true
else
    echo "❌ livecore FAILED - this is a CI failure"
    echo "Core dump generation is a critical requirement"
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi

# Create baseline with grf attach (the working way)
echo "Creating baseline with grf attach..."
GRF_PATH="$HOME/go/bin/grf"
if [ -x "$GRF_PATH" ]; then
    # First, create a baseline using grf attach
    echo "Running grf attach to create baseline..."
    if "$GRF_PATH" attach $SERVER_PID; then
        echo "✅ grf attach successful"
        if [ -f "grf.out" ]; then
            echo "✅ Baseline grf.out created"
            mv grf.out grf_baseline.out
            echo "Baseline grf.out size: $(wc -c < grf_baseline.out) bytes"
        else
            echo "❌ grf attach failed to create grf.out"
            kill $SERVER_PID 2>/dev/null || true
            exit 1
        fi
    else
        echo "❌ grf attach FAILED - this is a CI failure"
        kill $SERVER_PID 2>/dev/null || true
        exit 1
    fi
    
    # Now validate the core file with grf
    echo "Validating core file with grf..."
    if "$GRF_PATH" core ./test/httpserver/httpserver test_httpserver.core; then
        echo "✅ grf core validation successful"
        if [ -f "grf.out" ]; then
            echo "✅ Core grf.out file created successfully"
            echo "Core grf.out size: $(wc -c < grf.out) bytes"
            
            # Validate that both baseline and core dump contain expected functions using pprof
            echo "Validating baseline contents with pprof..."
            if go tool pprof -top grf_baseline.out 2>/dev/null | grep -q "wasteMemory"; then
                echo "✅ Baseline contains expected 'wasteMemory' function"
            else
                echo "❌ Baseline validation failed - 'wasteMemory' function not found"
                echo "Baseline pprof output:"
                go tool pprof -top grf_baseline.out 2>/dev/null || echo "pprof failed"
                kill $SERVER_PID 2>/dev/null || true
                exit 1
            fi
            
            echo "Validating core dump contents with pprof..."
            if go tool pprof -top grf.out 2>/dev/null | grep -q "wasteMemory"; then
                echo "✅ Core dump contains expected 'wasteMemory' function"
            else
                echo "❌ Core dump validation failed - 'wasteMemory' function not found"
                echo "Core dump pprof output:"
                go tool pprof -top grf.out 2>/dev/null || echo "pprof failed"
                kill $SERVER_PID 2>/dev/null || true
                exit 1
            fi
            
            # Show diff of pprof outputs for informational purposes (don't fail on differences)
            echo "Comparing pprof outputs (baseline vs core dump)..."
            echo "pprof diff (informational only):"
            diff -u <(go tool pprof -top grf_baseline.out 2>/dev/null) <(go tool pprof -top grf.out 2>/dev/null) || echo "pprof outputs differ (this is informational only)"
        else
            echo "❌ grf.out file not found - this is a CI failure"
            kill $SERVER_PID 2>/dev/null || true
            exit 1
        fi
    else
        echo "❌ grf core validation FAILED - this is a CI failure"
        echo "Core file validation is a critical requirement"
        kill $SERVER_PID 2>/dev/null || true
        exit 1
    fi
else
    echo "❌ grf tool not available - this is a CI failure"
    echo "grf tool is required for core file validation"
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi

# Verify the HTTP server is still running and responsive
echo "Verifying HTTP server is still running..."
if kill -0 $SERVER_PID 2>/dev/null; then
    echo "HTTP server process is still running"
    
    # Test server responsiveness again
    if curl -s http://127.0.0.1:$PORT/status >/dev/null; then
        echo "HTTP server is still responsive after core dump"
    else
        echo "Warning: HTTP server not responding after core dump"
    fi
else
    echo "Error: HTTP server process died during core dump"
    exit 1
fi

# Clean up
echo "Cleaning up test processes..."
kill $SERVER_PID 2>/dev/null || true
rm -f test/httpserver/httpserver test_httpserver.core grf.out grf_baseline.out

# Report final status
echo "✅ All core dump tests completed successfully"

echo "All CI checks passed!"
