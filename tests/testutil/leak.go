// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutil

import (
	"bytes"
	"fmt"
	"net/http"
	"regexp"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"
)

const (
	// DefaultPollTimeout is the maximum duration to wait for background goroutines to settle.
	DefaultPollTimeout = 2500 * time.Millisecond
	// DefaultPollInterval is the frequency of checking active goroutines.
	DefaultPollInterval = 25 * time.Millisecond
	// DefaultTolerance is the default allowed difference between baseline and final goroutine count.
	DefaultTolerance = 0
)

var (
	// hexAddrRegex matches hexadecimal memory addresses for stack normalization.
	hexAddrRegex = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	// goroutineIDRegex matches the goroutine ID line prefix.
	goroutineIDRegex = regexp.MustCompile(`^goroutine \d+ `)

	// defaultIgnoredPatterns lists function frames of Go runtime and test runner daemons.
	defaultIgnoredPatterns = []string{
		"runtime.forcegchelper",
		"runtime.bgsweep",
		"runtime.runfinq",
		"runtime.scavenger",
		"runtime.gcBgMarkWorker",
		"signal.signal_recv",
		"os/signal.loop",
		"runtime/pprof.writeGoroutineStacks",
		"runtime/pprof.Lookup",
		"testing.(*T).Run",
		"testing.tRunner",
		"testing.RunTests",
		"testing.(*M).Run",
		"aoni/tests/testutil.(*LeakTracker)",
		"aoni/tests/testutil.Check",
		"aoni/tests/testutil.VerifyRuntimeGoroutines",
		"foundation/silicon/clock",
		"foundation/timekit",
	}
)

// CheckConfig governs the leak verification behavior.
type CheckConfig struct {
	Timeout        time.Duration
	Interval       time.Duration
	Tolerance      int
	DrainFn        func()
	IgnorePatterns []string
}

// CheckOption mutates CheckConfig.
type CheckOption func(*CheckConfig)

// WithTimeout configures the maximum settling duration.
func WithTimeout(d time.Duration) CheckOption {
	return func(c *CheckConfig) { c.Timeout = d }
}

// WithInterval configures the polling interval.
func WithInterval(d time.Duration) CheckOption {
	return func(c *CheckConfig) { c.Interval = d }
}

// WithTolerance configures the allowed goroutine surplus.
func WithTolerance(tolerance int) CheckOption {
	return func(c *CheckConfig) { c.Tolerance = tolerance }
}

// WithDrain configures a custom connection drain hook.
func WithDrain(drain func()) CheckOption {
	return func(c *CheckConfig) { c.DrainFn = drain }
}

// WithIgnore appends a custom pattern to the ignored stack frames.
func WithIgnore(pattern string) CheckOption {
	return func(c *CheckConfig) { c.IgnorePatterns = append(c.IgnorePatterns, pattern) }
}

// Check snapshots active goroutines at invocation and returns a deferred assertion closure.
//
// Usage:
//
//	defer testutil.Check(t)()
//	defer testutil.Check(t, testutil.WithTolerance(1))()
func Check(t testing.TB, opts ...CheckOption) func() {
	t.Helper()
	tracker := NewLeakTracker(t, opts...)
	return func() {
		t.Helper()
		tracker.Verify()
	}
}

// VerifyRuntimeGoroutines records baseline and returns a deferred check with the given tolerance.
//
// Usage:
//
//	defer testutil.VerifyRuntimeGoroutines(t, 0)()
func VerifyRuntimeGoroutines(t testing.TB, tolerance int, opts ...CheckOption) func() {
	t.Helper()
	combinedOpts := append([]CheckOption{WithTolerance(tolerance)}, opts...)
	tracker := NewLeakTracker(t, combinedOpts...)
	return func() {
		t.Helper()
		tracker.Verify()
	}
}

// GoroutineInfo encapsulates a parsed goroutine stack trace.
type GoroutineInfo struct {
	Raw       string
	Signature string
}

// LeakTracker records baseline goroutines and validates post-test execution states.
type LeakTracker struct {
	t              testing.TB
	cfg            CheckConfig
	baselineCount  int
	baselineStacks map[string]int
}

// NewLeakTracker creates and snapshots baseline goroutines.
func NewLeakTracker(t testing.TB, opts ...CheckOption) *LeakTracker {
	t.Helper()

	cfg := CheckConfig{
		Timeout:   DefaultPollTimeout,
		Interval:  DefaultPollInterval,
		Tolerance: DefaultTolerance,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	drainDefaultTransport()
	if cfg.DrainFn != nil {
		cfg.DrainFn()
	}

	active := parseGoroutines(cfg.IgnorePatterns)
	baseMap := make(map[string]int, len(active))
	for _, g := range active {
		baseMap[g.Signature]++
	}

	return &LeakTracker{
		t:              t,
		cfg:            cfg,
		baselineCount:  len(active),
		baselineStacks: baseMap,
	}
}

// Verify polls until active goroutines settle to within baseline + tolerance, or fails the test.
// It verifies stack signatures against baseline to prevent terminated baseline routines
// from masking newly spawned leaks.
func (lt *LeakTracker) Verify() {
	lt.t.Helper()

	deadline := time.Now().Add(lt.cfg.Timeout)
	var finalActive []GoroutineInfo
	var totalLeaked int

	for {
		drainDefaultTransport()
		if lt.cfg.DrainFn != nil {
			lt.cfg.DrainFn()
		}

		finalActive = parseGoroutines(lt.cfg.IgnorePatterns)
		totalLeaked = lt.countLeaked(finalActive)
		if totalLeaked <= lt.cfg.Tolerance {
			return // Test passed cleanly
		}

		if time.Now().After(deadline) {
			break
		}

		runtime.Gosched()
		time.Sleep(lt.cfg.Interval)
	}

	// Verification failed: format failing goroutine stacks
	delta := totalLeaked
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "\n================================================================================\n")
	fmt.Fprintf(&buf, "GOROUTINE LEAK DETECTED in %s\n", lt.t.Name())
	fmt.Fprintf(&buf, "Baseline Count: %d | Final Count: %d | Leaked: +%d | Allowed Tolerance: %d\n",
		lt.baselineCount, len(finalActive), delta, lt.cfg.Tolerance)
	fmt.Fprintf(&buf, "Settling Timeout: %v\n", lt.cfg.Timeout)
	fmt.Fprintf(&buf, "================================================================================\n\n")

	fmt.Fprintf(&buf, "Active Non-Ignored Goroutines (%d total):\n\n", len(finalActive))
	for i, g := range finalActive {
		isNew := lt.baselineStacks[g.Signature] == 0
		newMarker := ""
		if isNew {
			newMarker = " [NEW - NOT PRESENT AT BASELINE]"
		}
		fmt.Fprintf(&buf, "--- Goroutine #%d%s ---\n%s\n\n", i+1, newMarker, g.Raw)
	}
	fmt.Fprintf(&buf, "================================================================================\n")

	lt.t.Fatalf("%s", buf.String())
}

// countLeaked returns the total number of goroutines whose stack signatures
// exceed their baseline count. Baseline routines that terminated do not offset new leaks.
func (lt *LeakTracker) countLeaked(active []GoroutineInfo) int {
	activeCounts := make(map[string]int, len(active))
	for _, g := range active {
		activeCounts[g.Signature]++
	}

	leaked := 0
	for sig, count := range activeCounts {
		baseCount := lt.baselineStacks[sig]
		if count > baseCount {
			leaked += count - baseCount
		}
	}
	return leaked
}

func drainDefaultTransport() {
	if tr, ok := http.DefaultTransport.(*http.Transport); ok {
		tr.CloseIdleConnections()
	}
}

func parseGoroutines(customIgnores []string) []GoroutineInfo {
	var buf bytes.Buffer
	p := pprof.Lookup("goroutine")
	if p == nil {
		return nil
	}
	_ = p.WriteTo(&buf, 2)

	rawText := strings.ReplaceAll(buf.String(), "\r\n", "\n")
	blocks := strings.Split(rawText, "\n\n")

	var active []GoroutineInfo
	for _, block := range blocks {
		trimmed := strings.TrimSpace(block)
		if trimmed == "" || !strings.HasPrefix(trimmed, "goroutine ") {
			continue
		}

		if isIgnored(trimmed, customIgnores) {
			continue
		}

		sig := normalizeSignature(trimmed)
		active = append(active, GoroutineInfo{
			Raw:       trimmed,
			Signature: sig,
		})
	}

	return active
}

func isIgnored(stack string, customIgnores []string) bool {
	for _, pattern := range defaultIgnoredPatterns {
		if strings.Contains(stack, pattern) {
			return true
		}
	}
	for _, pattern := range customIgnores {
		if strings.Contains(stack, pattern) {
			return true
		}
	}
	return false
}

func normalizeSignature(stack string) string {
	lines := strings.Split(stack, "\n")
	var normLines []string
	for i, line := range lines {
		if i == 0 {
			line = goroutineIDRegex.ReplaceAllString(line, "goroutine ")
		}
		line = hexAddrRegex.ReplaceAllString(line, "0x_")
		normLines = append(normLines, strings.TrimSpace(line))
	}
	return strings.Join(normLines, "\n")
}
