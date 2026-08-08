// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package geoip_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/telemetry/geoip"
)

func TestDB_NilSafety(t *testing.T) {
	t.Parallel()

	var db *geoip.DB
	meta, err := db.Lookup(netip.MustParseAddr("8.8.8.8"))
	assert.NoError(t, err)
	assert.Nil(t, meta)

	assert.NoError(t, db.Close())
}

func TestOpen_InvalidPath(t *testing.T) {
	t.Parallel()

	_, err := geoip.Open("non_existent_file.mmdb")
	assert.Error(t, err)

	_, errBytes := geoip.OpenBytes([]byte("invalid mmdb bytes"))
	assert.Error(t, errBytes)
}
