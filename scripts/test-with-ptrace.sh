#!/bin/bash

set -e

echo "🧪 Testing livecore with ptrace enabled..."

# Enable ptrace
echo "Enabling ptrace..."
echo 0 > /proc/sys/kernel/yama/ptrace_scope
echo "✅ Ptrace scope set to: $(cat /proc/sys/kernel/yama/ptrace_scope)"

# Build livecore
echo "Building livecore..."
go build -o livecore .

# Build HTTP server
echo "Building HTTP server..."
cd test/httpserver
go build -o httpserver .

# Find available port
PORT=8081
while netstat -ln 2>/dev/null | grep -q ":$PORT "; do
    PORT=$((PORT + 1))
done

# Start HTTP server
echo "Starting HTTP server on port $PORT..."
./httpserver $PORT &
SERVER_PID=$!

# Wait for server to start
echo "Waiting for server to start..."
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

# Test livecore
echo "Testing livecore against HTTP server (PID: $SERVER_PID)..."
cd ../..
if ./livecore -verbose -passes 2 -dirty-thresh 10 $SERVER_PID test_httpserver.core; then
    echo "✅ livecore completed successfully!"
    
    # Check if core file was created
    if [ -f "test_httpserver.core" ]; then
        echo "✅ Core file created: $(ls -lh test_httpserver.core)"
    else
        echo "❌ Core file not created"
        exit 1
    fi
    
    # Validate with grf if available
    if command -v grf >/dev/null 2>&1; then
        echo "Validating core file with grf..."
        if grf core ./test/httpserver/httpserver test_httpserver.core; then
            echo "✅ grf validation successful"
            if [ -f "grf.out" ]; then
                echo "✅ grf.out created: $(ls -lh grf.out)"
            fi
        else
            echo "❌ grf validation failed"
            exit 1
        fi
    else
        echo "⚠️  grf tool not available, skipping validation"
    fi
else
    echo "❌ livecore failed"
    exit 1
fi

# Verify HTTP server is still running
echo "Verifying HTTP server is still running..."
if kill -0 $SERVER_PID 2>/dev/null; then
    echo "✅ HTTP server is still running"
    if curl -s http://127.0.0.1:$PORT/status >/dev/null; then
        echo "✅ HTTP server is still responsive"
    else
        echo "⚠️  HTTP server not responding"
    fi
else
    echo "❌ HTTP server died during core dump"
    exit 1
fi

# Clean up
echo "Cleaning up..."
kill $SERVER_PID 2>/dev/null || true
rm -f test/httpserver/httpserver test_httpserver.core grf.out

echo "🎉 All tests passed! livecore is working correctly."
echo "⚠️  Remember to re-enable security: sudo sysctl kernel.yama.ptrace_scope=1"

