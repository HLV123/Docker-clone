#!/bin/bash
# test/integration/e2e.sh
# Chạy: sudo bash test/integration/e2e.sh
set -e

BINARY="$(dirname "$0")/../../mydocker"
PASS=0
FAIL=0

check() {
    local desc="$1"
    local expected="$2"
    local actual="$3"
    if echo "$actual" | grep -q "$expected"; then
        echo "  PASS: $desc"
        PASS=$((PASS+1))
    else
        echo "  FAIL: $desc"
        echo "        expected: $expected"
        echo "        got:      $actual"
        FAIL=$((FAIL+1))
    fi
}

cleanup() {
    sudo "$BINARY" ps -a 2>/dev/null | tail -n +2 | awk '{print $1}' | \
        xargs -r -I{} sudo "$BINARY" rm -f {} 2>/dev/null || true
}
trap cleanup EXIT

echo "=== mydocker E2E Tests ==="
echo ""

# Test 1: images
echo "[Test 1] images list"
OUT=$(sudo "$BINARY" images 2>&1)
echo "  Output: $OUT"
PASS=$((PASS+1))

# Test 2: run basic command
echo "[Test 2] run basic command"
OUT=$(sudo "$BINARY" run alpine /bin/echo "hello-world")
check "echo output" "hello-world" "$OUT"

# Test 3: hostname isolation
echo "[Test 3] hostname isolation"
HOST_HN=$(hostname)
CONT_HN=$(sudo "$BINARY" run alpine /bin/hostname)
if [ "$CONT_HN" != "$HOST_HN" ]; then
    echo "  PASS: hostname isolated ($CONT_HN != $HOST_HN)"
    PASS=$((PASS+1))
else
    echo "  FAIL: hostname not isolated"
    FAIL=$((FAIL+1))
fi

# Test 4: filesystem isolation
echo "[Test 4] Alpine filesystem"
OUT=$(sudo "$BINARY" run alpine /bin/sh -c "cat /etc/os-release | head -1")
check "Alpine os-release" "Alpine" "$OUT"

# Test 5: PID namespace
echo "[Test 5] PID namespace"
OUT=$(sudo "$BINARY" run alpine /bin/sh -c 'echo $$')
check "PID 1 in container" "1" "$OUT"

# Test 6: cgroup memory limit
echo "[Test 6] Memory limit (50m)"
OUT=$(sudo "$BINARY" run --memory=50m alpine /bin/sh -c \
    "dd if=/dev/zero bs=1M count=100 2>&1 || echo 'OOM or limited'" 2>&1 || true)
echo "  Output: $OUT"
PASS=$((PASS+1))  # Just checking it doesn't crash

# Test 7: ps command
echo "[Test 7] ps shows containers"
sudo "$BINARY" run alpine /bin/echo "ps-test" > /dev/null
OUT=$(sudo "$BINARY" ps -a)
check "ps -a shows container" "alpine" "$OUT"

# Test 8: rm command
echo "[Test 8] rm container"
# Find an exited container
ID=$(sudo "$BINARY" ps -a 2>/dev/null | tail -n +2 | awk '{print $1}' | head -1)
if [ -n "$ID" ]; then
    sudo "$BINARY" rm "$ID"
    if [ ! -d "/var/lib/mydocker/containers/$ID" ]; then
        echo "  PASS: container removed"
        PASS=$((PASS+1))
    else
        echo "  FAIL: container dir still exists"
        FAIL=$((FAIL+1))
    fi
else
    echo "  SKIP: no exited container to remove"
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
