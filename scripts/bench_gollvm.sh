#!/usr/bin/env bash
# Copyright (c) 2026 Lemon4ksan All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

set -e

echo "=== System & Compiler Information ==="
echo "Date: $(date)"
echo "Host: $(uname -a)"
echo "CPU: $(lscpu | grep 'Model name' | sed 's/Model name:[ \t]*//')"

echo ""
echo "=== Standard Go (gc) ==="
/usr/bin/go version

echo ""
echo "=== Gollvm (LLVM) ==="
export PATH="/home/senya/gollvm-install/bin:$PATH"
export LD_LIBRARY_PATH="/home/senya/gollvm-install/lib64:$LD_LIBRARY_PATH"
llvm-goc -v 2>&1 | head -n 3
/home/senya/gollvm-install/bin/go version

echo ""
echo "=== Testing Gollvm basic compilation ==="
cat << 'EOF' > /tmp/gollvm_test.go
package main
import "fmt"
func main() {
    fmt.Println("Gollvm Hello World OK")
}
EOF
export GOWORK=off
export GOPATH=/mnt/c/Users/senya/go
export GOPROXY=https://proxy.golang.org,direct

echo ""
echo "=== 1. Testing Gollvm dynamic build (-O3 -march=skylake) ==="
mkdir -p /tmp/gollvm_demo
cd /tmp/gollvm_demo
cat << 'EOF' > main.go
package main

import (
	"fmt"
	"time"
)

func compute(n int) int {
	sum := 0
	for i := 0; i < n; i++ {
		sum += (i * 3) ^ (i >> 2)
	}
	return sum
}

func main() {
	start := time.Now()
	res := compute(100000000)
	dur := time.Since(start)
	fmt.Printf("Gollvm compute result: %d in %v\n", res, dur)
}
EOF

/home/senya/gollvm-install/bin/go build -gccgoflags="-O3 -march=skylake" -o main_dyn main.go
./main_dyn

echo ""
echo "=== 2. Testing Gollvm static build (-O3 -static-libgo) ==="
/home/senya/gollvm-install/bin/go build -gccgoflags="-O3 -static-libgo" -o main_static main.go
./main_static
ldd main_static 2>&1 || true

echo ""
echo "=== 3. Testing direct LLVM assembly generation (llvm-goc) ==="
llvm-goc -fgo-pkgpath=main -O3 -march=skylake -S -o output.s main.go
head -n 25 output.s

rm -rf /tmp/gollvm_demo
echo ""
echo "=== All Gollvm build modes validated successfully! ==="

