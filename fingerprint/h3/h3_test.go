// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"

	"github.com/lemon4ksan/aoni/fingerprint/h3"
)

func TestH3Settings_Presets(t *testing.T) {
	t.Parallel()

	assert.Equal(t, uint64(6291456), h3.ChromeSettings.InitialStreamReceiveWindow)
	assert.Equal(t, uint64(15728640), h3.ChromeSettings.InitialConnectionReceiveWindow)
	assert.True(t, h3.ChromeSettings.EnableDatagrams)

	assert.Equal(t, uint64(6291456), h3.FirefoxSettings.InitialStreamReceiveWindow)
	assert.Equal(t, uint64(25165824), h3.FirefoxSettings.InitialConnectionReceiveWindow)
	assert.False(t, h3.FirefoxSettings.EnableDatagrams)
}
