#!/bin/bash

# Test that demonstrates parent-child ptrace relationship
echo "Testing parent-child ptrace relationship..."

# Start HTTP server as child of livecore process
cd test/httpserver
go build -o httpserver .

# Find available port
PORT=8081
while netstat -ln 2>/dev/null | grep -q ":$PORT "; do
    PORT=$((PORT + 1))
done

echo "Starting HTTP server on port $PORT as child process..."
./httpserver $PORT &
SERVER_PID=$!

# Wait for server to start
for i in {1..30}; do
    if curl -s http://127.0.0.1:$PORT/status >/dev/null 2>&1; then
        echo "HTTP server is responsive"
        break
    fi
    if [ $i -eq 30 ]; then
        echo "Error: HTTP server not responding"
        kill $SERVER_PID 2>/dev/null || true
        exit 1
    fi
    sleep 0.1
done

# Now run livecore - it should work since we're the parent
cd ../..
echo "Running livecore against child process..."
if ./livecore -verbose $SERVER_PID test_parent_child.core; then
    echo "✅ livecore succeeded with parent-child relationship"
    
    # Validate with grf
    if command -v grf >/dev/null 2>&1; then
        if grf core ./test/httpserver/httpserver test_parent_child.core; then
            echo "✅ grf validation successful"
        else
            echo "❌ grf validation failed"
        fi
    fi
else
    echo "❌ livecore failed even with parent-child relationship"
fi

# Clean up
kill $SERVER_PID 2>/dev/null || true
rm -f test_parent_child.core grf.out
