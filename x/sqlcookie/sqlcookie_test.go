// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlcookie_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/x/sqlcookie"
)

func TestSQLStorage_Interface(t *testing.T) {
	t.Parallel()

	s := sqlcookie.New(nil)
	assert.NotNil(t, s)

	var _ cookie.Storage = s

	custom := sqlcookie.NewWithTable(nil, "my_cookies")
	assert.NotNil(t, custom)

	emptyTable := sqlcookie.NewWithTable(nil, "")
	assert.NotNil(t, emptyTable)
}
