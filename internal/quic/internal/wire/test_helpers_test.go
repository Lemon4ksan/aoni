// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire

import (
	"bytes"
	"encoding/binary"
	"log"
	"testing"

	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
	"github.com/lemon4ksan/aoni/internal/quic/internal/utils"
	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

func encodeVarInt(i uint64) []byte {
	return quicvarint.Append(nil, i)
}

func appendVersion(data []byte, v protocol.Version) []byte {
	offset := len(data)
	data = append(data, []byte{0, 0, 0, 0}...)
	binary.BigEndian.PutUint32(data[offset:], uint32(v))

	return data
}

func setupLogTest(t *testing.T, buf *bytes.Buffer) utils.Logger {
	logger := utils.DefaultLogger
	logger.SetLogLevel(utils.LogLevelDebug)

	originalOutput := log.Writer()

	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(originalOutput) })

	return logger
}
