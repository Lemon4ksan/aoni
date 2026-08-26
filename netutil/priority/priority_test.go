// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package priority_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/netutil/priority"
)

func TestPriority_Format(t *testing.T) {
	tests := []struct {
		p        priority.Priority
		expected string
	}{
		{priority.New(0, false), "u=0"},
		{priority.New(1, true), "u=1, i"},
		{priority.New(3, true), "u=3, i"},
		{priority.New(5, false), "u=5"},
		{priority.New(-1, false), "u=0"},
		{priority.New(10, true), "u=7, i"},
	}

	for _, tt := range tests {
		got := tt.p.Format()
		if got != tt.expected {
			t.Errorf("Format(%v) = %q, want %q", tt.p, got, tt.expected)
		}
	}
}

func TestPriority_Parse(t *testing.T) {
	tests := []struct {
		input string
		wantU int
		wantI bool
	}{
		{"u=0", 0, false},
		{"u=1, i", 1, true},
		{"i, u=2", 2, true},
		{"u=5", 5, false},
		{"u=3, i=?1", 3, true},
		{"u=4, i=?0", 4, false},
		{"", 3, false},
	}

	for _, tt := range tests {
		got, err := priority.Parse(tt.input)
		if err != nil {
			t.Errorf("Parse(%q) returned error: %v", tt.input, err)
		}
		if got.Urgency != tt.wantU || got.Incremental != tt.wantI {
			t.Errorf("Parse(%q) = {%d, %v}, want {%d, %v}", tt.input, got.Urgency, got.Incremental, tt.wantU, tt.wantI)
		}
	}
}
