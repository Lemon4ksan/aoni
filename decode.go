// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

// Decoder defines an interface for decoding response bodies.
type Decoder interface {
	Decode(r io.Reader, target any) error
}

// DecoderFunc is a function type that implements Decoder.
type DecoderFunc func(r io.Reader, target any) error

// Decode implements the Decoder interface for DecoderFunc.
func (f DecoderFunc) Decode(r io.Reader, target any) error {
	return f(r, target)
}

// RawDecoder returns the response body as a byte slice.
// It expects target to be a pointer to a byte slice (*[]byte).
var RawDecoder Decoder = rawDecoder{}

type rawDecoder struct{}

func (rawDecoder) Decode(r io.Reader, target any) error {
	ptr, ok := target.(*[]byte)
	if !ok {
		return fmt.Errorf("aoni: RawDecoder requires *[]byte as output type, got %T", target)
	}

	buf := bufferPool.Get().(*bytes.Buffer)

	buf.Reset()
	defer bufferPool.Put(buf)

	bufPtr := bytePool.Get().(*[]byte)
	defer bytePool.Put(bufPtr)

	_, err := io.CopyBuffer(buf, r, *bufPtr)
	if err != nil {
		return err
	}

	data := make([]byte, buf.Len())
	copy(data, buf.Bytes())
	*ptr = data

	return nil
}

// JSONDecoder is the default decoder that uses encoding/json.
var JSONDecoder Decoder = DecoderFunc(func(r io.Reader, target any) error {
	return json.NewDecoder(r).Decode(target)
})

// ProtobufDecoder decodes Protobuf data. It automatically detects if the
// source is JSON-encoded Protobuf or standard binary wire format.
var ProtobufDecoder Decoder = DecoderFunc(func(r io.Reader, target any) error {
	pm, ok := target.(proto.Message)
	if !ok {
		return errors.New("aoni: target is not a proto.Message")
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if len(data) > 0 && data[0] == '{' {
		return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(data, pm)
	}

	return proto.Unmarshal(data, pm)
})

// XMLDecoder decodes XML data.
var XMLDecoder Decoder = DecoderFunc(func(r io.Reader, target any) error {
	return xml.NewDecoder(r).Decode(target)
})

// YAMLDecoder decodes YAML data.
var YAMLDecoder Decoder = DecoderFunc(func(r io.Reader, target any) error {
	return yaml.NewDecoder(r).Decode(target)
})

// AsProtobuf returns a RequestModifier that sets the decoder to ProtobufDecoder.
func AsProtobuf() RequestModifier { return WithDecoder(ProtobufDecoder) }

// AsRaw returns a RequestModifier that sets the decoder to RawDecoder.
func AsRaw() RequestModifier { return WithDecoder(RawDecoder) }

// AsXML returns a RequestModifier that sets the decoder to XMLDecoder.
func AsXML() RequestModifier { return WithDecoder(XMLDecoder) }

// AsYAML returns a RequestModifier that sets the decoder to YAMLDecoder.
func AsYAML() RequestModifier { return WithDecoder(YAMLDecoder) }
