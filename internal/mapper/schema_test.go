// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mapper_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/mapper"
)

type sampleUser struct {
	ID    int    `url:"id"`
	Name  string `url:"name,omitempty"`
	Email string `url:"-"`
}

func TestSchemaCache(t *testing.T) {
	typ := reflect.TypeOf(sampleUser{})

	s1 := mapper.DefaultSchemaCache.GetSchema(typ)
	require.NotNil(t, s1)
	assert.Len(t, s1.Fields, 3)

	s2 := mapper.DefaultSchemaCache.GetSchema(typ)
	assert.Same(t, s1, s2)
}
