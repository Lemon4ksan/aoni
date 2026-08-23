// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAckRangeLength(t *testing.T) {
	require.EqualValues(t, 1, AckRange{Smallest: 10, Largest: 10}.Len())
	require.EqualValues(t, 4, AckRange{Smallest: 10, Largest: 13}.Len())
}
