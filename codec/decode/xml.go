// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode

import (
	"encoding/xml"
	"io"

	"github.com/lemon4ksan/foundation/refkit"
)

// xmlDecoder unmarshals XML response streams into Go target structs.
type xmlDecoder struct{}

func (xmlDecoder) Decode(reader io.Reader, target any) error {
	if data, _, ok := InspectBytes(reader); ok {
		if len(data) == 0 {
			return nil
		}

		return xml.Unmarshal(StripBOMBytes(data), target)
	}

	return xml.NewDecoder(StripBOM(reader)).Decode(target)
}

// rawDecoder reads raw response body payload bytes directly into a *[]byte target.
type rawDecoder struct{}

func (rawDecoder) Decode(r io.Reader, target any) error {
	switch outPtr := target.(type) {
	case *[]byte:
		rawBytes, err := ReadAllSafe(r)
		if err != nil {
			return &Error{Format: "raw", Target: refkit.FullTypeName(target), Err: err}
		}

		*outPtr = rawBytes

		return nil

	case **[]byte:
		rawBytes, err := ReadAllSafe(r)
		if err != nil {
			return &Error{Format: "raw", Target: refkit.FullTypeName(target), Err: err}
		}

		if *outPtr == nil {
			*outPtr = new([]byte)
		}

		**outPtr = rawBytes

		return nil

	default:
		return &Error{Format: "raw", Target: refkit.FullTypeName(target), Err: ErrInvalidRawTarget}
	}
}
