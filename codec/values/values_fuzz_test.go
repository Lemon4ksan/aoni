// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package values_test

import (
	"strings"
	"testing"

	"github.com/lemon4ksan/aoni/codec/values"
)

type FuzzItem struct {
	Name    string   `query:"name,omitempty"`
	Age     int      `query:"age"`
	Tags    []string `query:"tags,comma"`
	Active  bool     `query:"active"`
	Score   float64  `query:"score"`
	Default string   `query:"def,default=default_val"`
}

func FuzzValuesEncode(f *testing.F) {
	f.Add("alice", 30, "tag1,tag2", true, 95.5)
	f.Add("", 0, "", false, 0.0)
	f.Add("special&key=val?query", -1, "a,b,c", true, -0.001)

	f.Fuzz(func(t *testing.T, name string, age int, tags string, active bool, score float64) {
		item := FuzzItem{
			Name:   name,
			Age:    age,
			Tags:   strings.Split(tags, ","),
			Active: active,
			Score:  score,
		}

		vals, err := values.Encode(item)
		if err == nil && vals != nil {
			_ = vals.Encode()
		}

		var sb strings.Builder

		_ = values.EncodeQueryString(item, &sb)

		m := map[string]string{
			"key1": name,
			"key2": tags,
		}
		_, _ = values.Encode(m)
	})
}
