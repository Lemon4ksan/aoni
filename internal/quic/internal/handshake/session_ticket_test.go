// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handshake

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/quic/internal/wire"
	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

func TestMarshalUnmarshalSessionTicket(t *testing.T) {
	ticket := &sessionTicket{
		Parameters: &wire.TransportParameters{
			InitialMaxStreamDataBidiLocal:  1,
			InitialMaxStreamDataBidiRemote: 2,
			ActiveConnectionIDLimit:        10,
			MaxDatagramFrameSize:           20,
		},
	}

	var t2 sessionTicket
	require.NoError(t, t2.Unmarshal(ticket.Marshal()))
	require.EqualValues(t, 1, t2.Parameters.InitialMaxStreamDataBidiLocal)
	require.EqualValues(t, 2, t2.Parameters.InitialMaxStreamDataBidiRemote)
	require.EqualValues(t, 10, t2.Parameters.ActiveConnectionIDLimit)
	require.EqualValues(t, 20, t2.Parameters.MaxDatagramFrameSize)
}

func TestUnmarshalRefusesTooShortTicket(t *testing.T) {
	err := (&sessionTicket{}).Unmarshal([]byte{})
	require.EqualError(t, err, "failed to read session ticket revision")
}

func TestUnmarshalRefusesUnknownRevision(t *testing.T) {
	b := quicvarint.Append(nil, 1337)
	err := (&sessionTicket{}).Unmarshal(b)
	require.EqualError(t, err, "unknown session ticket revision: 1337")
}

func TestUnmarshal0RTTRefusesInvalidTransportParameters(t *testing.T) {
	b := quicvarint.Append(nil, sessionTicketRevision)
	b = append(b, []byte("foobar")...)
	err := (&sessionTicket{}).Unmarshal(b)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unmarshaling transport parameters from session ticket failed")
}
