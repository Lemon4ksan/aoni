#!/usr/bin/env bash
# Copyright (c) 2026 Lemon4ksan All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

set -e

export PATH="/home/senya/gollvm-install/bin:$PATH"
export LD_LIBRARY_PATH="/home/senya/gollvm-install/lib64:$LD_LIBRARY_PATH"
export GOWORK=off
export GOTOOLCHAIN=local

BIN_DIR="/tmp/gollvm_bench_bins"
mkdir -p "$BIN_DIR"

echo "================================================================="
echo "       AONI SILICON BENCHMARK: Standard Go vs Gollvm (LLVM)      "
echo "================================================================="
echo "CPU: $(lscpu | grep 'Model name' | sed 's/Model name:[ \t]*//')"
echo "Standard Go: $(/usr/bin/go version)"
echo "Gollvm:      $(/home/senya/gollvm-install/bin/go version)"
echo "================================================================="

cd /mnt/d/CodingProjects/aoni/scripts/gollvm_bench

echo ""
echo "[1/3] Building with standard Go (gc)..."
/usr/bin/go build -o "$BIN_DIR/bench_gc" main.go

echo "[2/3] Building with Gollvm (-O3 -march=skylake)..."
/home/senya/gollvm-install/bin/go build -gccgoflags="-O3 -march=skylake" -o "$BIN_DIR/bench_gollvm" main.go

echo "[3/3] Running Benchmarks..."
echo ""
echo ">>> Running Standard Go (gc) Benchmark:"
"$BIN_DIR/bench_gc"

echo ""
echo ">>> Running Gollvm (-O3 -march=skylake) Benchmark:"
"$BIN_DIR/bench_gollvm"

echo ""
echo "================================================================="
echo "Benchmark run completed."
