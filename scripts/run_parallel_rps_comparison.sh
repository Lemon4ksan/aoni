#!/usr/bin/env bash
# Copyright (c) 2026 Lemon4ksan All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

set -e

export PATH="/home/senya/gollvm-install/bin:$PATH"
export LD_LIBRARY_PATH="/home/senya/gollvm-install/lib64:$LD_LIBRARY_PATH"
export GOWORK=off
export GOTOOLCHAIN=local

BIN_DIR="/tmp/gollvm_rps_bins"
mkdir -p "$BIN_DIR"

echo "================================================================="
echo "   GLOBAL PARALLEL MULTI-CORE RPS BENCHMARK: Go (gc) vs Gollvm   "
echo "================================================================="
echo "CPU: $(lscpu | grep 'Model name' | sed 's/Model name:[ \t]*//')"
echo "================================================================="

cd /mnt/d/CodingProjects/aoni/scripts/gollvm_bench

echo ""
echo "[1/4] Building Pure HTTP RPS harness..."
/usr/bin/go build -o "$BIN_DIR/rps_gc" parallel_rps.go
/home/senya/gollvm-install/bin/go build -gccgoflags="-O3 -march=skylake" -o "$BIN_DIR/rps_gollvm" parallel_rps.go

echo "[2/4] Building Full Protocol Pipeline (HTTP + HPACK + Varint + Match) harness..."
/usr/bin/go build -o "$BIN_DIR/pipe_gc" parallel_pipeline_rps.go
/home/senya/gollvm-install/bin/go build -gccgoflags="-O3 -march=skylake" -o "$BIN_DIR/pipe_gollvm" parallel_pipeline_rps.go

echo "[3/4] Running Pure HTTP Stress Tests (2,400,000 requests across 12 cores)..."
echo ""
echo ">>> [1/2] Standard Go (gc) Pure HTTP:"
"$BIN_DIR/rps_gc"
echo ""
echo ">>> [2/2] Gollvm (-O3) Pure HTTP:"
"$BIN_DIR/rps_gollvm"

echo ""
echo "[4/4] Running Full Protocol Pipeline Tests (1,200,000 full transactions across 12 cores)..."
echo ""
echo ">>> [1/2] Standard Go (gc) Full Pipeline:"
"$BIN_DIR/pipe_gc"
echo ""
echo ">>> [2/2] Gollvm (-O3) Full Pipeline:"
"$BIN_DIR/pipe_gollvm"

echo ""
echo "================================================================="
echo "All Parallel RPS benchmarks completed."
