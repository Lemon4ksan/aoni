// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "strings"

// StringSliceFlag implements [flag.Value] for multi-valued command line arguments.
type StringSliceFlag []string

func (s *StringSliceFlag) String() string {
	if s == nil {
		return ""
	}

	return strings.Join(*s, ",")
}

func (s *StringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}
