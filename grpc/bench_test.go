// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc_test

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/lemon4ksan/aoni/grpc"
)

func BenchmarkMarshalFrame_Uncompressed(b *testing.B) {
	msg := wrapperspb.String("benchmark test protobuf message payload string")

	b.ReportAllocs()

	for b.Loop() {
		frame, err := grpc.MarshalFrame(msg, false)
		if err != nil {
			b.Fatal(err)
		}

		if len(frame) == 0 {
			b.Fatal("empty frame")
		}
	}
}

func BenchmarkMarshalFrame_Compressed(b *testing.B) {
	msg := wrapperspb.String("benchmark test protobuf message payload string with compression enabled")

	b.ReportAllocs()

	for b.Loop() {
		frame, err := grpc.MarshalFrame(msg, true)
		if err != nil {
			b.Fatal(err)
		}

		if len(frame) == 0 {
			b.Fatal("empty frame")
		}
	}
}

func BenchmarkUnmarshalFrame_Uncompressed(b *testing.B) {
	msg := wrapperspb.String("benchmark test protobuf message payload string")

	frame, err := grpc.MarshalFrame(msg, false)
	if err != nil {
		b.Fatal(err)
	}

	r := bytes.NewReader(frame)

	var target wrapperspb.StringValue

	b.ReportAllocs()

	for b.Loop() {
		r.Reset(frame)

		_, err := grpc.UnmarshalFrame(r, &target)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalFrame_Compressed(b *testing.B) {
	msg := wrapperspb.String("benchmark test protobuf message payload string with compression")

	frame, err := grpc.MarshalFrame(msg, true)
	if err != nil {
		b.Fatal(err)
	}

	r := bytes.NewReader(frame)

	var target wrapperspb.StringValue

	b.ReportAllocs()

	for b.Loop() {
		r.Reset(frame)

		_, err := grpc.UnmarshalFrame(r, &target)
		if err != nil {
			b.Fatal(err)
		}
	}
}
