// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode

import (
	"bytes"
	stdio "io"
	"reflect"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/internal/pipeline"
)

// protoDecoder unmarshals binary Protocol Buffer response streams into [proto.Message] targets.
type protoDecoder struct{}

func (protoDecoder) Decode(r stdio.Reader, target any) error {
	msg, err := castOrResolveProto(target)
	if err != nil {
		return err
	}

	buf, err := copyToBuffer(r)
	if err != nil {
		return err
	}
	defer pipeline.GlobalBufferPool.Put(buf)

	if err := proto.Unmarshal(buf.Bytes(), msg); err != nil {
		return &Error{Format: "proto", Target: typeName(msg), Err: err}
	}

	return nil
}

// protoJSONDecoder unmarshals JSON response streams into [proto.Message] targets using protojson options.
type protoJSONDecoder struct{}

func (protoJSONDecoder) Decode(r stdio.Reader, target any) error {
	msg, err := castOrResolveProto(target)
	if err != nil {
		return err
	}

	buf, err := copyToBuffer(r)
	if err != nil {
		return err
	}
	defer pipeline.GlobalBufferPool.Put(buf)

	opts := protojson.UnmarshalOptions{DiscardUnknown: true}

	if err := opts.Unmarshal(buf.Bytes(), msg); err != nil {
		return &Error{Format: "protojson", Target: typeName(msg), Err: err}
	}

	return nil
}

// copyToBuffer streams r contents into a pooled byte buffer using zero-allocation copying.
func copyToBuffer(r stdio.Reader) (*bytes.Buffer, error) {
	buf := pipeline.GlobalBufferPool.Get()

	if _, err := io.CopyZeroAlloc(buf, r); err != nil {
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
		elem := val.Elem()
		if elem.Kind() == reflect.Pointer && elem.IsNil() && elem.CanSet() {
			elem.Set(reflect.New(elem.Type().Elem()))

			if msg, ok := elem.Interface().(proto.Message); ok {
				return msg, nil
			}
		}
	}

	return nil, &Error{Format: "proto", Target: typeName(target), Err: ErrInvalidProtoTarget}
}
