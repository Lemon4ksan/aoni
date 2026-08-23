// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ossfuzzseeds writes Go native fuzz seeds as OSS-Fuzz seed corpus files.
package ossfuzzseeds

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// ErrUnencodableDynamicCorpusArgs is returned when OSS-Fuzz's dynamic argument
// weight format can't exactly represent the requested dynamic argument lengths.
var ErrUnencodableDynamicCorpusArgs = errors.New("dynamic corpus arguments can't be encoded for OSS-Fuzz")

// Helper wraps *testing.F.
// When FUZZ_CORPUS_DIR is set to a non-empty path, Add() writes corpus entries
// in the raw format OSS-Fuzz expects.
type Helper struct {
	*testing.F
	corpusDir string
	enabled   bool
}

// New creates a helper.
// If FUZZ_CORPUS_DIR is set, corpus files will be written to that directory.
func New(f *testing.F) *Helper {
	dir := os.Getenv("FUZZ_CORPUS_DIR")
	enabled := dir != ""

	h := &Helper{
		F:         f,
		corpusDir: dir,
		enabled:   enabled,
	}

	if enabled {
		if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec
			f.Errorf("failed to create corpus directory %s: %v", dir, err)
		}
	}

	return h
}

// Add calls the real Add and, if enabled, also writes the corpus entry.
func (h *Helper) Add(args ...any) {
	h.F.Add(args...)

	if h.enabled {
		h.writeCorpusEntry(args...)
	}
}

// writeCorpusEntry writes the arguments using the raw OSS-Fuzz corpus format.
func (h *Helper) writeCorpusEntry(args ...any) {
	entry, err := CorpusEntry(args...)
	if err != nil {
		h.Errorf("failed to encode corpus entry: %v", err)
		return
	}

	filename := sha256Name(entry)
	path := filepath.Join(h.corpusDir, filename)

	h.Logf("writing corpus entry %s", path)

	if err := os.WriteFile(path, entry, 0o644); err != nil {
		h.Errorf("failed to write corpus entry %s: %v", path, err)
	}
}

// CorpusEntry encodes one set of testing.F.Add arguments as a raw OSS-Fuzz
// seed corpus entry.
//
// This is the lower-level encoder used by Helper.Add when FUZZ_CORPUS_DIR is
// set. Most fuzz targets should use New(f).Add(...) instead.
func CorpusEntry(args ...any) ([]byte, error) {
	var fixed []byte

	var dynamic [][]byte

	for _, arg := range args {
		switch v := arg.(type) {
		case []byte:
			dynamic = append(dynamic, v)
		case string:
			dynamic = append(dynamic, []byte(v))
		default:
			var err error

			fixed, err = appendFixedSizeArg(fixed, arg)
			if err != nil {
				return nil, err
			}
		}
	}

	weights, err := dynamicSizeWeights(dynamic)
	if err != nil {
		return nil, err
	}

	totalSize := len(fixed) + len(weights)
	for _, data := range dynamic {
		totalSize += len(data)
	}

	entry := make([]byte, 0, totalSize)
	entry = append(entry, fixed...)
	entry = append(entry, weights...)

	for _, data := range dynamic {
		entry = append(entry, data...)
	}

	return entry, nil
}

func appendFixedSizeArg(dst []byte, arg any) ([]byte, error) {
	switch v := arg.(type) {
	case bool:
		if v {
			return append(dst, 1), nil
		}

		return append(dst, 0), nil

	case int:
		return binary.BigEndian.AppendUint64(dst, uint64(int64(v))), nil
	case int8:
		return append(dst, byte(v)), nil
	case int16:
		return binary.BigEndian.AppendUint16(dst, uint16(v)), nil
	case int32:
		return binary.BigEndian.AppendUint32(dst, uint32(v)), nil
	case int64:
		return binary.BigEndian.AppendUint64(dst, uint64(v)), nil
	case uint:
		return binary.BigEndian.AppendUint64(dst, uint64(v)), nil
	case uint8:
		return append(dst, v), nil
	case uint16:
		return binary.BigEndian.AppendUint16(dst, v), nil
	case uint32:
		return binary.BigEndian.AppendUint32(dst, v), nil
	case uint64:
		return binary.BigEndian.AppendUint64(dst, v), nil
	case float32:
		return binary.BigEndian.AppendUint32(dst, math.Float32bits(v)), nil
	case float64:
		return binary.BigEndian.AppendUint64(dst, math.Float64bits(v)), nil
	default:
		return nil, fmt.Errorf("unsupported corpus argument type %T", arg)
	}
}

// dynamicSizeWeights returns the byte-sized weights that input.Source needs to
// split entry bytes back into the given dynamic arguments.
//
// The consumer reads one byte-sized weight per dynamic argument and uses
//
//	argSize[i] = total * weight[i] / sum   (integer division)
//
// for every argument except the last; the last receives all remaining bytes.
// We need weights w[i] in [0, 255] with sum S such that for i in [0, n-2]:
//
//	floor(total * w[i] / S) == lengths[i]
//
// The last weight has no length constraint; it just needs to be byte-sized.
//
// This is not always possible: a single byte per weight only gives so much
// resolution. For example, lengths {1, 1, 100000} would require w[0]/S in
// [1/100002, 2/100002), which no S <= 255*3 can express. Such inputs return
// ErrUnencodableDynamicCorpusArgs.
func dynamicSizeWeights(dynamic [][]byte) ([]byte, error) {
	n := len(dynamic)
	if n == 0 {
		return nil, nil
	}

	lengths := make([]int, n)
	total, maxLen := 0, 0

	for i, data := range dynamic {
		lengths[i] = len(data)
		total += len(data)

		if len(data) > maxLen {
			maxLen = len(data)
		}
	}

	// Fast path: when every length fits in a byte, picking S = total and
	// w[i] = lengths[i] satisfies the formula exactly, since
	// floor(total * lengths[i] / total) == lengths[i]. This covers the
	// overwhelming majority of realistic seed corpus entries.
	if maxLen <= 255 {
		weights := make([]byte, n)
		for i, l := range lengths {
			weights[i] = byte(l)
		}

		return weights, nil
	}

	// Slow path: at least one dynamic argument is longer than 255 bytes, so
	// we have to find a smaller weight sum. Try each candidate in order; the
	// search space is small (at most 255*n).
	for sum := 1; sum <= 255*n; sum++ {
		if weights, ok := weightsForSum(lengths, total, sum); ok {
			return weights, nil
		}
	}

	return nil, fmt.Errorf("%w: lengths %v", ErrUnencodableDynamicCorpusArgs, lengths)
}

// weightsForSum tries to build a valid weight assignment for the given sum.
// For each non-last argument the valid weight range is
//
//	w[i] in [ceil(L*sum/total), floor(((L+1)*sum-1)/total)] ∩ [0, 255]
//
// and the last weight w[n-1] = sum - Σ w[:n-1] must be in [0, 255].
func weightsForSum(lengths []int, total, sum int) ([]byte, bool) {
	n := len(lengths)
	weights := make([]byte, n)
	his := make([]int, n-1)

	var prefLo, prefHi int

	for i, l := range lengths[:n-1] {
		lo := (l*sum + total - 1) / total
		hi := min(((l+1)*sum-1)/total, 255)

		if lo > hi {
			return nil, false
		}

		weights[i] = byte(lo)
		his[i] = hi
		prefLo += lo
		prefHi += hi
	}

	// We need Σ w[:n-1] in [max(0, sum-255), min(sum, prefHi)] so that the
	// last weight is byte-sized; pick the smallest valid prefix sum and
	// distribute the slack greedily across the prefix weights.
	prefix := max(prefLo, sum-255)
	if prefix > prefHi || prefix > sum {
		return nil, false
	}

	slack := prefix - prefLo
	for i := 0; i < n-1 && slack > 0; i++ {
		add := min(slack, his[i]-int(weights[i]))
		weights[i] += byte(add)
		slack -= add
	}

	weights[n-1] = byte(sum - prefix)

	return weights, true
}

// sha256Name returns a short, stable, unique filename using SHA-256.
// We only use the first 8 bytes (16 hex chars) — more than enough for
// corpus filenames and avoids unnecessarily long names.
func sha256Name(entry []byte) string {
	h := sha256.New()
	h.Write(entry)
	sum := h.Sum(nil)

	return hex.EncodeToString(sum[:8])
}
