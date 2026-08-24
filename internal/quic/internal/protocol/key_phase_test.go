// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestKeyPhaseBitDefaultValue(t *testing.T) {
	var k KeyPhaseBit
	require.Equal(t, KeyPhaseUndefined, k)
}

func TestKeyPhaseStringRepresentation(t *testing.T) {
	require.Equal(t, "0", KeyPhaseZero.String())
	require.Equal(t, "1", KeyPhaseOne.String())
}

func TestKeyPhaseToBit(t *testing.T) {
	require.Equal(t, KeyPhaseZero, KeyPhase(0).Bit())
	require.Equal(t, KeyPhaseZero, KeyPhase(2).Bit())
	require.Equal(t, KeyPhaseZero, KeyPhase(4).Bit())
	require.Equal(t, KeyPhaseOne, KeyPhase(1).Bit())
	require.Equal(t, KeyPhaseOne, KeyPhase(3).Bit())
	require.Equal(t, KeyPhaseOne, KeyPhase(5).Bit())
}
