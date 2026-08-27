// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package secret_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/netutil/secret"
)

func TestSecret_Masking(t *testing.T) {
	t.Parallel()

	sec := secret.New("super-secret-api-token")

	// Raw value
	assert.Equal(t, "super-secret-api-token", sec.Value())
	assert.Equal(t, "super-secret-api-token", sec.Expose())

	// String & Printf masking
	assert.Equal(t, "******", sec.String())
	assert.Equal(t, "token: ******", fmt.Sprintf("token: %s", sec))
	assert.Equal(t, "token: ******", fmt.Sprintf("token: %v", sec))
	assert.Equal(t, "secret.Secret(******)", fmt.Sprintf("%#v", sec))

	// JSON masking
	marshaled, err := json.Marshal(sec)
	require.NoError(t, err)
	assert.Equal(t, `"******"`, string(marshaled))

	// JSON Unmarshal
	var unmarshaled secret.Secret[string]

	err = json.Unmarshal([]byte(`"new-secret"`), &unmarshaled)
	require.NoError(t, err)
	assert.Equal(t, "new-secret", unmarshaled.Value())
}
