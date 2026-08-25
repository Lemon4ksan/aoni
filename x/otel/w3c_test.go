// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otel

import (
	"testing"
)

func TestW3C_TraceParent_Roundtrip(t *testing.T) {
	rawHeader := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	sc, err := ParseTraceParent(rawHeader)
	if err != nil {
		t.Fatalf("unexpected error parsing valid traceparent: %v", err)
	}

	if !sc.IsValid() {
		t.Fatal("expected SpanContext to be valid")
	}

	if sc.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("expected traceID 4bf92f3577b34da6a3ce929d0e0e4736, got %s", sc.TraceID().String())
	}

	if sc.SpanID().String() != "00f067aa0ba902b7" {
		t.Errorf("expected spanID 00f067aa0ba902b7, got %s", sc.SpanID().String())
	}

	if !sc.IsSampled() {
		t.Errorf("expected trace to be sampled")
	}

	formatted := sc.TraceParent()
	if formatted != rawHeader {
		t.Errorf("expected formatted traceparent %s, got %s", rawHeader, formatted)
	}
}

func TestW3C_TraceParent_Invalid(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"empty", ""},
		{"too_short", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7"},
		{"version_ff", "ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
		{"zero_trace_id", "00-00000000000000000000000000000000-00f067aa0ba902b7-01"},
		{"zero_span_id", "00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01"},
		{"invalid_hex_trace", "00-4bf92f3577b34da6a3ce929d0e0e47zz-00f067aa0ba902b7-01"},
		{"invalid_hex_span", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902zz-01"},
		{"invalid_flags", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-zz"},
		{"extra_parts_v00", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-extra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTraceParent(tt.header)
			if err == nil {
				t.Fatalf("expected error parsing invalid header %q, got nil", tt.header)
			}
		})
	}
}

func TestW3C_TraceID_SpanID_Generation(t *testing.T) {
	tID1 := NewTraceID()
	tID2 := NewTraceID()

	if !tID1.IsValid() || !tID2.IsValid() {
		t.Fatal("generated IDs must be valid")
	}

	if tID1 == tID2 {
		t.Fatal("subsequent trace IDs must be uniquely random")
	}

	sID1 := NewSpanID()
	sID2 := NewSpanID()

	if !sID1.IsValid() || !sID2.IsValid() {
		t.Fatal("generated span IDs must be valid")
	}

	if sID1 == sID2 {
		t.Fatal("subsequent span IDs must be uniquely random")
	}
}

func BenchmarkTraceParentFormat(b *testing.B) {
	sc := NewSpanContext(NewTraceID(), NewSpanID(), FlagSampled, "", false)
	b.ReportAllocs()
	for b.Loop() {
		_ = sc.TraceParent()
	}
}

func BenchmarkTraceParentParse(b *testing.B) {
	rawHeader := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	b.ReportAllocs()
	for b.Loop() {
		_, _ = ParseTraceParent(rawHeader)
	}
}
