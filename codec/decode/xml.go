// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode

import (
	"encoding/xml"
	stdio "io"
)

type xmlDecoder struct{}

func (xmlDecoder) Decode(reader stdio.Reader, target any) error {
	return xml.NewDecoder(StripBOM(reader)).Decode(target)
}

type rawDecoder struct{}

func (rawDecoder) Decode(r stdio.Reader, target any) error {
	outPtr, ok := target.(*[]byte)
	if !ok {
		return &Error{Format: "raw", Target: typeName(target), Err: ErrInvalidRawTarget}
	}

	rawBytes, err := stdio.ReadAll(r)
	if err != nil {
		return &Error{Format: "raw", Target: typeName(target), Err: err}
	}

	*outPtr = rawBytes

	return nil
}
