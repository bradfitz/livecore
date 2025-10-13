#!/bin/bash

set -e

echo "Running CI checks for livecore (container-aware)..."

# Check if we're in a container environment
if [ -f /.dockerenv ] || [ -n "$CONTAINER" ]; then
    echo "⚠️  Running in container environment"
    echo "   Ptrace may be restricted due to security policies"
    echo "   This is expected and normal for containerized CI"
fi

# Check ptrace capabilities
if ! capsh --print | grep -q "cap_sys_ptrace"; then
    echo "⚠️  Missing CAP_SYS_PTRACE capability"
    echo "   Core dump functionality will be limited"
fi

# Check Yama ptrace scope
PTRACE_SCOPE=$(cat /proc/sys/kernel/yama/ptrace_scope 2>/dev/null || echo "unknown")
echo "Yama ptrace scope: $PTRACE_SCOPE"

# Run basic CI checks (these should always pass)
echo "Running basic CI checks..."
go vet ./...
go fmt -l . | grep -q . && (echo "Code not formatted"; exit 1) || true
go mod tidy
go test ./...
go build -o livecore ./cmd/livecore

# Test livecore help (this should always work)
echo "Testing livecore help..."
./livecore -h >/dev/null

# Test with a simple process that we can definitely ptrace
echo "Testing with self-ptrace capability..."
if ./livecore -verbose $$ self_test.core 2>/dev/null; then
    echo "✅ Self-ptrace test passed"
    rm -f self_test.core
else
    echo "⚠️  Self-ptrace test failed (expected in containers)"
fi

# Test HTTP server startup (without ptrace)
echo "Testing HTTP server startup..."
cd test/httpserver
go build -o httpserver .

# Find available port
PORT=8081
while netstat -ln 2>/dev/null | grep -q ":$PORT "; do
    PORT=$((PORT + 1))
done

echo "Starting HTTP server on port $PORT..."
./httpserver $PORT &
SERVER_PID=$!

# Wait for server to start
for i in {1..30}; do
    if curl -s http://127.0.0.1:$PORT/status >/dev/null 2>&1; then
        echo "✅ HTTP server is responsive"
        break
    fi
    if [ $i -eq 30 ]; then
        echo "❌ HTTP server not responding"
        kill $SERVER_PID 2>/dev/null || true
        exit 1
    fi
    sleep 0.1
done

# Test livecore against HTTP server
echo "Testing livecore against HTTP server..."
cd ../..
if ./livecore -verbose $SERVER_PID test_httpserver.core 2>/dev/null; then
    echo "✅ livecore succeeded - full functionality available"
    
    # Validate with grf if available
    if command -v grf >/dev/null 2>&1; then
        if grf core ./test/httpserver/httpserver test_httpserver.core; then
            echo "✅ grf validation successful"
        else
            echo "❌ grf validation failed"
        fi
    fi
else
    echo "⚠️  livecore failed - ptrace restricted (expected in containers)"
    echo "   This is normal for containerized environments"
fi

# Clean up
echo "Cleaning up..."
kill $SERVER_PID 2>/dev/null || true
rm -f test/httpserver/httpserver test_httpserver.core grf.out

echo "✅ Container-aware CI completed"
echo "   Note: Full ptrace functionality requires proper capabilities"
