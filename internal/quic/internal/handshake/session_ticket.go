// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handshake

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/lemon4ksan/aoni/internal/quic/internal/wire"
	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

const sessionTicketRevision = 5

type sessionTicket struct {
	Parameters *wire.TransportParameters
}

func (t *sessionTicket) Marshal() []byte {
	b := make([]byte, 0, 256)
	b = quicvarint.Append(b, sessionTicketRevision)
	return t.Parameters.MarshalForSessionTicket(b)
}

func (t *sessionTicket) Unmarshal(b []byte) error {
	rev, l, err := quicvarint.Parse(b)
	if err != nil {
		return errors.New("failed to read session ticket revision")
	}

	b = b[l:]

	if rev != sessionTicketRevision {
		return fmt.Errorf("unknown session ticket revision: %d", rev)
	}

	var tp wire.TransportParameters
	if err := tp.UnmarshalFromSessionTicket(b); err != nil {
		return fmt.Errorf("unmarshaling transport parameters from session ticket failed: %s", err.Error())
	}

	t.Parameters = &tp

	return nil
}

const extraPrefix = "quic-go1"

func addSessionStateExtraPrefix(b []byte) []byte {
	return append([]byte(extraPrefix), b...)
}

func findSessionStateExtraData(extras [][]byte) []byte {
	prefix := []byte(extraPrefix)
	for _, extra := range extras {
		if data, ok := bytes.CutPrefix(extra, prefix); ok {
			return data
		}
	}

	return nil
}
