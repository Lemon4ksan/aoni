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

// NormalizeArgs stitches back arguments that were fragmented by shell tokenizers (e.g. PowerShell splitting -out=pkg/api and .go).
func NormalizeArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}

	result := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]
		for i+1 < len(args) && strings.HasPrefix(args[i+1], ".") && !strings.Contains(args[i+1], "/") && !strings.Contains(args[i+1], "\\") {
			// Orphaned suffix like .go, .json, .har, .yaml
			arg = strings.Join([]string{arg, args[i+1]}, "")
			i++
		}

		result = append(result, arg)
	}

	return result
}
