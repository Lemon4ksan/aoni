// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/fingerprint/h2"
)

func FuzzParseSettings(f *testing.F) {
	f.Add(`{"header_table_size":65536,"enable_push":0,"initial_window_size":6291456,"max_header_list_size":262144,"connection_flow":15663105,"priority_weight":255,"priority_exclusive":true}`)
	f.Add(`{"HeaderTableSize":65536,"EnablePush":0}`)
	f.Add(`{}`)
	f.Add(`not a json string`)
	f.Add(`{"header_table_size":"string_instead_of_number"}`)

	f.Fuzz(func(t *testing.T, jsonStr string) {
		settings, err := h2.ParseSettings(jsonStr)
		if err == nil {
			_ = settings.HeaderTableSize
			_ = settings.EnablePush
			_ = settings.InitialWindowSize
			_ = settings.PriorityWeight
		}
	})
}
