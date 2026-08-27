// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode

import (
	"bytes"
	"io"
	"reflect"

	fio "github.com/lemon4ksan/foundation/iokit"
	"github.com/lemon4ksan/foundation/refkit"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/internal/pipeline"
)

// protoDecoder unmarshals binary Protocol Buffer response streams into [proto.Message] targets.
type protoDecoder struct{}

func (protoDecoder) Decode(r io.Reader, target any) error {
	msg, err := castOrResolveProto(target)
	if err != nil {
		return err
	}

	if data, _, ok := InspectBytes(r); ok {
		if err := proto.Unmarshal(data, msg); err != nil {
			return &Error{Format: "proto", Target: refkit.FullTypeName(msg), Err: err}
		}

		return nil
	}

	buf, err := copyToBuffer(r)
	if err != nil {
		return err
	}
	defer pipeline.GlobalBufferPool.Put(buf)

	if err := proto.Unmarshal(buf.Bytes(), msg); err != nil {
		return &Error{Format: "proto", Target: refkit.FullTypeName(msg), Err: err}
	}

	return nil
}

// protoJSONDecoder unmarshals JSON response streams into [proto.Message] targets using protojson options.
type protoJSONDecoder struct{}

func (protoJSONDecoder) Decode(r io.Reader, target any) error {
	msg, err := castOrResolveProto(target)
	if err != nil {
		return err
	}

	opts := protojson.UnmarshalOptions{DiscardUnknown: true}

	if data, _, ok := InspectBytes(r); ok {
		if err := opts.Unmarshal(data, msg); err != nil {
			return &Error{Format: "protojson", Target: refkit.FullTypeName(msg), Err: err}
		}

		return nil
	}

	buf, err := copyToBuffer(r)
	if err != nil {
		return err
	}
	defer pipeline.GlobalBufferPool.Put(buf)

	if err := opts.Unmarshal(buf.Bytes(), msg); err != nil {
		return &Error{Format: "protojson", Target: refkit.FullTypeName(msg), Err: err}
	}

	return nil
}

// copyToBuffer streams r contents into a pooled byte buffer using zero-allocation copying.
func copyToBuffer(r io.Reader) (*bytes.Buffer, error) {
	buf := pipeline.GlobalBufferPool.Get()

	if _, err := fio.CopyZeroAlloc(buf, r); err != nil {
		pipeline.GlobalBufferPool.Put(buf)
		return nil, err
	}

	return buf, nil
}

// castOrResolveProto type-asserts target to [proto.Message] or initializes nil pointer targets via reflection.
func castOrResolveProto(target any) (proto.Message, error) {
	if msg, ok := target.(proto.Message); ok {
		return msg, nil
	}

	val := reflect.ValueOf(target)
	if val.Kind() == reflect.Pointer && !val.IsNil() {
		elem, _ := refkit.EnsureAlloc(val.Elem())
		if elem.IsValid() && elem.CanAddr() {
			if msg, ok := elem.Addr().Interface().(proto.Message); ok {
				return msg, nil
			}
		}

		if elem.IsValid() && elem.CanInterface() {
			if msg, ok := elem.Interface().(proto.Message); ok {
				return msg, nil
			}
		}
	}

	return nil, &Error{Format: "proto", Target: refkit.TypeName(target), Err: ErrInvalidProtoTarget}
}
