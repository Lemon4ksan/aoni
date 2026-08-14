// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ip_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/netutil/ip"
)

func BenchmarkSourceIPRotator_Next_Serial(b *testing.B) {
	rot, err := ip.NewSourceIPRotator([]string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = rot.Next()
	}
}

func BenchmarkSourceIPRotator_Next_Parallel(b *testing.B) {
	rot, err := ip.NewSourceIPRotator([]string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = rot.Next()
		}
	})
}

func BenchmarkSourceIPRotator_NextForFamily_Parallel(b *testing.B) {
	rot, err := ip.NewSourceIPRotator([]string{"10.0.0.1", "2001:db8::1", "10.0.0.2", "2001:db8::2"})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = rot.NextForFamily(true)
		}
	})
}
