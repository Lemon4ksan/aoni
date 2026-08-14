// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode

import (
	"encoding/xml"
	stdio "io"
)

// bufferedBytesReader is implemented by response body wrappers that expose pre-buffered
// payload bytes to avoid io.ReadAll growing-buffer overhead.
type bufferedBytesReader interface {
	// Bytes returns the buffered payload and whether the backing store is off-heap.
	// When onOffHeap is true, callers must copy before the buffer is released.
	Bytes() (data []byte, onOffHeap bool)
}

// xmlDecoder unmarshals XML response streams into Go target structs.
type xmlDecoder struct{}

func (xmlDecoder) Decode(reader stdio.Reader, target any) error {
	return xml.NewDecoder(StripBOM(reader)).Decode(target)
}

// rawDecoder reads raw response body payload bytes directly into a *[]byte target.
type rawDecoder struct{}

func (rawDecoder) Decode(r stdio.Reader, target any) error {
	outPtr, ok := target.(*[]byte)
	if !ok {
		return &Error{Format: "raw", Target: typeName(target), Err: ErrInvalidRawTarget}
	}

	// Fast-path: reader exposes pre-buffered bytes - avoid io.ReadAll's growing-buffer allocs.
	if br, ok := r.(bufferedBytesReader); ok {
		data, onOffHeap := br.Bytes()
		if len(data) > 0 {
			if onOffHeap {
				// Off-heap data must be copied before the backing page is released.
				clone := make([]byte, len(data))
				copy(clone, data)
				*outPtr = clone
			} else {
				// Go-heap data: safe to reference directly.
				*outPtr = data
			}

			return nil
		}
	}

	rawBytes, err := stdio.ReadAll(r)
	if err != nil {
		return &Error{Format: "raw", Target: typeName(target), Err: err}
	}

	*outPtr = rawBytes

	return nil
}
