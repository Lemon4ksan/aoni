// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mapper_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/internal/mapper"
)

type SampleQuery struct {
	Page    int    `url:"page"`
	Search  string `url:"q,omitempty"`
	Ignored string `url:"-"`
}

func TestSchemaCache(t *testing.T) {
	t.Parallel()

	cache := &mapper.SchemaCache{}

	s := SampleQuery{Page: 2, Search: "golang", Ignored: "secret"}
	m := cache.MapStructToMap(s, "url")

	assert.Equal(t, "2", m["page"])
	assert.Equal(t, "golang", m["q"])
	_, hasIgnored := m["Ignored"]
	assert.False(t, hasIgnored)
}
