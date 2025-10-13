#!/bin/bash

echo "Enabling ptrace for livecore testing..."
echo "Current ptrace scope: $(cat /proc/sys/kernel/yama/ptrace_scope)"

# Disable Yama ptrace scope
echo 0 > /proc/sys/kernel/yama/ptrace_scope
echo "New ptrace scope: $(cat /proc/sys/kernel/yama/ptrace_scope)"

echo "✅ Ptrace enabled - livecore should now work"
echo "⚠️  Remember to re-enable security after testing:"
echo "   sudo sysctl kernel.yama.ptrace_scope=1"

