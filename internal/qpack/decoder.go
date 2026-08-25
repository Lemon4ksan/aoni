// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package qpack

import (
	"errors"
	"io"
)

var (
	errInvalidHeaderBlock = errors.New("qpack: invalid header block")
	errInvalidStaticIndex = errors.New("qpack: invalid static table index")
)

// Decoder parses QPACK header blocks (RFC 9204 §4.5).
type Decoder struct{}

// NewDecoder creates a new QPACK Decoder.
func NewDecoder() *Decoder {
	return &Decoder{}
}

// DecodeFields iterates over all QPACK header fields in data, calling emit for each field with zero closure allocations.
func (d *Decoder) DecodeFields(data []byte, emit func(hf HeaderField) bool) error {
	if len(data) == 0 {
		return nil
	}

	offset := 0

	// RFC 9204 §4.5.1: Encoded Field Section Prefix
	// 1. Required Insert Count (8-bit prefix integer)
	_, n1, err := readInt(8, data[offset:])
	if err != nil {
		return err
	}

	offset += n1
	if offset >= len(data) {
		return io.EOF
	}

	// 2. Base (Delta Base: 7-bit prefix integer)
	_, n2, err := readInt(7, data[offset:])
	if err != nil {
		return err
	}

	offset += n2

	for offset < len(data) {
		b := data[offset]

		switch {
		case (b & 0x80) != 0:
			// RFC 9204 §4.5.2: Indexed Field Line
			isStatic := (b & 0x40) != 0

			idx, n, err := readInt(6, data[offset:])
			if err != nil {
				return err
			}

			offset += n

			if isStatic {
				if int(idx) >= len(staticTable) {
					return errInvalidStaticIndex
				}

				if !emit(staticTable[idx]) {
					return nil
				}
			} else {
				return errInvalidHeaderBlock
			}

		case (b & 0xc0) == 0x40:
			// RFC 9204 §4.5.4: Literal Field Line With Name Reference
			isStatic := (b & 0x10) != 0

			idx, n, err := readInt(4, data[offset:])
			if err != nil {
				return err
			}

			offset += n

			var name string
			if isStatic {
				if int(idx) >= len(staticTable) {
					return errInvalidStaticIndex
				}

				name = staticTable[idx].Name
			} else {
				return errInvalidHeaderBlock
			}

			val, vn, err := readString(data[offset:])
			if err != nil {
				return err
			}

			offset += vn

			if !emit(HeaderField{Name: name, Value: val}) {
				return nil
			}

		case (b&0xe0) == 0x20 || (b&0xf0) == 0x00:
			// RFC 9204 §4.5.6: Literal Field Line With Literal Name
			isHuffmanName := (b & 0x08) != 0

			nameLen, n, err := readInt(3, data[offset:])
			if err != nil {
				return err
			}

			offset += n
			if offset+int(nameLen) > len(data) {
				return io.ErrUnexpectedEOF
			}

			nameRaw := data[offset : offset+int(nameLen)]
			offset += int(nameLen)

			var name string
			if isHuffmanName {
				name, err = decodeHuffman(nameRaw)
				if err != nil {
					return err
				}
			} else {
				name = string(nameRaw)
			}

			val, vn, err := readString(data[offset:])
			if err != nil {
				return err
			}

			offset += vn

			if !emit(HeaderField{Name: name, Value: val}) {
				return nil
			}

		default:
			return errInvalidHeaderBlock
		}
	}

	return nil
}

// Decode returns an iterator function that parses HeaderField items sequentially until io.EOF.
func (d *Decoder) Decode(data []byte) func() (HeaderField, error) {
	offset := 0
	prefixParsed := false

	return func() (HeaderField, error) {
		if offset >= len(data) {
			return HeaderField{}, io.EOF
		}

		if !prefixParsed {
			// RFC 9204 §4.5.1: Encoded Field Section Prefix
			// 1. Required Insert Count (8-bit prefix integer)
			_, n1, err := readInt(8, data[offset:])
			if err != nil {
				return HeaderField{}, err
			}

			offset += n1
			if offset >= len(data) {
				return HeaderField{}, io.EOF
			}

			// 2. Base (Delta Base: 7-bit prefix integer)
			_, n2, err := readInt(7, data[offset:])
			if err != nil {
				return HeaderField{}, err
			}

			offset += n2
			prefixParsed = true

			if offset >= len(data) {
				return HeaderField{}, io.EOF
			}
		}

		b := data[offset]

		switch {
		case (b & 0x80) != 0:
			// RFC 9204 §4.5.2: Indexed Field Line
			// 1 T [6-bit Index]
			isStatic := (b & 0x40) != 0

			idx, n, err := readInt(6, data[offset:])
			if err != nil {
				return HeaderField{}, err
			}

			offset += n

			if isStatic {
				if int(idx) >= len(staticTable) {
					return HeaderField{}, errInvalidStaticIndex
				}

				return staticTable[idx], nil
			}

			return HeaderField{}, errInvalidHeaderBlock

		case (b & 0xc0) == 0x40:
			// RFC 9204 §4.5.4: Literal Field Line With Name Reference
			// 0 1 N T [4-bit Index]
			isStatic := (b & 0x10) != 0

			idx, n, err := readInt(4, data[offset:])
			if err != nil {
				return HeaderField{}, err
			}

			offset += n

			var name string
			if isStatic {
				if int(idx) >= len(staticTable) {
					return HeaderField{}, errInvalidStaticIndex
				}

				name = staticTable[idx].Name
			} else {
				return HeaderField{}, errInvalidHeaderBlock
			}

			val, vn, err := readString(data[offset:])
			if err != nil {
				return HeaderField{}, err
			}

			offset += vn

			return HeaderField{Name: name, Value: val}, nil

		case (b&0xe0) == 0x20 || (b&0xf0) == 0x00:
			// RFC 9204 §4.5.6: Literal Field Line With Literal Name
			// 0 0 1 N H [3-bit Name Length] OR 0 0 0 0 H [3-bit Name Length]
			isHuffmanName := (b & 0x08) != 0

			nameLen, n, err := readInt(3, data[offset:])
			if err != nil {
				return HeaderField{}, err
			}

			offset += n
			if offset+int(nameLen) > len(data) {
				return HeaderField{}, io.ErrUnexpectedEOF
			}

			nameRaw := data[offset : offset+int(nameLen)]
			offset += int(nameLen)

			var name string
			if isHuffmanName {
				name, err = decodeHuffman(nameRaw)
				if err != nil {
					return HeaderField{}, err
				}
			} else {
				name = string(nameRaw)
			}

			val, vn, err := readString(data[offset:])
			if err != nil {
				return HeaderField{}, err
			}

			offset += vn

			return HeaderField{Name: name, Value: val}, nil

		default:
			return HeaderField{}, errInvalidHeaderBlock
		}
	}
}
