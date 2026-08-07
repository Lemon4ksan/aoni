// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/klauspost/compress/gzip"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/typepb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type errorReader struct{}

func (errorReader) Read(_ []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestErrorStructures(t *testing.T) {
	t.Parallel()

	t.Run("DecodeError_formatting", func(t *testing.T) {
		t.Parallel()

		var nilErr *Error
		assert.Equal(t, "<nil>", nilErr.Error())

		errWithTarget := &Error{Format: "proto", Target: "*pb.User", Err: ErrInvalidProtoTarget}
		assert.Equal(
			t,
			"aoni: decode proto into *pb.User: aoni: ProtoDecoder requires proto.Message output target",
			errWithTarget.Error(),
		)

		errWithoutTarget := &Error{Format: "json", Err: io.EOF}
		assert.Equal(t, "aoni: decode json: EOF", errWithoutTarget.Error())

		assert.Equal(t, ErrInvalidProtoTarget, errWithTarget.Unwrap())
	})

	t.Run("GRPCWebError_formatting", func(t *testing.T) {
		t.Parallel()

		var nilErr *GRPCWebError
		assert.Equal(t, "<nil>", nilErr.Error())

		errWithStatus := &GRPCWebError{StatusCode: "16", StatusMsg: "unauthenticated", Err: ErrGRPCWebStatusError}
		assert.Equal(
			t,
			"aoni: grpc-web status=16 msg=unauthenticated: aoni: gRPC-Web endpoint returned error status",
			errWithStatus.Error(),
		)

		errWithOp := &GRPCWebError{Op: "decompress", Err: io.ErrUnexpectedEOF}
		assert.Equal(t, "aoni: grpc-web decompress: unexpected EOF", errWithOp.Error())

		errFallback := &GRPCWebError{Err: ErrInvalidGRPCWebFrame}
		assert.Equal(t, "aoni: grpc-web: aoni: invalid gRPC-Web frame format", errFallback.Error())

		assert.Equal(t, ErrGRPCWebStatusError, errWithStatus.Unwrap())
	})
}

func TestRawDecoder_Decode(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader("raw payload data")

		var output []byte

		err := RawDecoder.Decode(r, &output)
		require.NoError(t, err)
		assert.Equal(t, "raw payload data", string(output))
		assert.True(t, IsRawDecoder(RawDecoder))
	})

	t.Run("invalid_target_type", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader("some data")

		var output string

		err := RawDecoder.Decode(r, &output)
		assert.Error(t, err)

		var decErr *Error
		require.ErrorAs(t, err, &decErr)
		assert.ErrorIs(t, decErr, ErrInvalidRawTarget)
	})

	t.Run("copy_error", func(t *testing.T) {
		t.Parallel()

		var output []byte

		err := RawDecoder.Decode(errorReader{}, &output)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})
}

func TestProtoDecoder_Decode(t *testing.T) {
	t.Parallel()

	t.Run("success_direct_message", func(t *testing.T) {
		t.Parallel()

		expected := wrapperspb.String("hello_proto")
		data, err := proto.Marshal(expected)
		require.NoError(t, err)

		var target wrapperspb.StringValue

		err = ProtoDecoder.Decode(bytes.NewReader(data), &target)
		require.NoError(t, err)
		assert.Equal(t, "hello_proto", target.GetValue())
	})

	t.Run("success_pointer_resolution", func(t *testing.T) {
		t.Parallel()

		expected := wrapperspb.String("hello_pointer")
		data, err := proto.Marshal(expected)
		require.NoError(t, err)

		var target *wrapperspb.StringValue // nil pointer

		err = ProtoDecoder.Decode(bytes.NewReader(data), &target)
		require.NoError(t, err)
		require.NotNil(t, target)
		assert.Equal(t, "hello_pointer", target.GetValue())
	})

	t.Run("invalid_target_type", func(t *testing.T) {
		t.Parallel()

		var invalidTarget string

		err := ProtoDecoder.Decode(strings.NewReader("data"), &invalidTarget)
		assert.Error(t, err)

		var decErr *Error
		require.ErrorAs(t, err, &decErr)
		assert.ErrorIs(t, decErr, ErrInvalidProtoTarget)
	})

	t.Run("corrupted_proto_data", func(t *testing.T) {
		t.Parallel()

		var target wrapperspb.StringValue

		err := ProtoDecoder.Decode(strings.NewReader("\xFF\xFF\xFF\xFF"), &target)
		assert.Error(t, err)
	})

	t.Run("io_read_error", func(t *testing.T) {
		t.Parallel()

		var target wrapperspb.StringValue

		err := ProtoDecoder.Decode(errorReader{}, &target)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})
}

func TestJSONDecoder_CustomConfig(t *testing.T) {
	t.Parallel()

	t.Run("disallow_unknown_fields", func(t *testing.T) {
		t.Parallel()

		type User struct {
			Name string `json:"name"`
		}

		dec := NewJSONDecoder(JSONDecoderConfig{DisallowUnknownFields: true})

		var u User

		err := dec.Decode(strings.NewReader(`{"name":"Alice","extra":123}`), &u)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown field")
	})

	t.Run("use_number", func(t *testing.T) {
		t.Parallel()

		type User struct {
			ID any `json:"id"`
		}

		dec := NewJSONDecoder(JSONDecoderConfig{UseNumber: true})

		var u User

		err := dec.Decode(strings.NewReader(`{"id":1234567890123456789}`), &u)
		require.NoError(t, err)

		num, ok := u.ID.(json.Number)
		require.True(t, ok)
		assert.Equal(t, "1234567890123456789", num.String())
	})
}

func TestProtoJSONDecoder_Decode(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		jsonPayload := `{"name":"proto_json_test","unknownField":123}`

		var target typepb.Option

		err := ProtoJSONDecoder.Decode(strings.NewReader(jsonPayload), &target)
		require.NoError(t, err)
		assert.Equal(t, "proto_json_test", target.GetName())
	})

	t.Run("invalid_json", func(t *testing.T) {
		t.Parallel()

		var target typepb.Option

		err := ProtoJSONDecoder.Decode(strings.NewReader(`{"name":`), &target)
		assert.Error(t, err)
	})
}

func TestXMLDecoder_Decode(t *testing.T) {
	t.Parallel()

	type Item struct {
		Name  string  `xml:"name"`
		Price float64 `xml:"price"`
	}

	xmlData := `<Item><name>Laptop</name><price>999.99</price></Item>`

	var item Item

	err := XMLDecoder.Decode(strings.NewReader(xmlData), &item)
	require.NoError(t, err)
	assert.Equal(t, "Laptop", item.Name)
	assert.Equal(t, 999.99, item.Price)

	val, err := XML[Item](strings.NewReader(xmlData))
	require.NoError(t, err)
	assert.Equal(t, "Laptop", val.Name)
}

func TestGRPCWebDecoder_Decode(t *testing.T) {
	t.Parallel()

	buildFrame := func(flags byte, payload []byte) []byte {
		buf := make([]byte, 5+len(payload))
		buf[0] = flags
		binary.BigEndian.PutUint32(buf[1:5], uint32(len(payload))) //nolint:gosec
		copy(buf[5:], payload)

		return buf
	}

	t.Run("uncompressed_binary_frame_success", func(t *testing.T) {
		t.Parallel()

		pbData, _ := proto.Marshal(wrapperspb.String("grpc_binary"))
		frame := buildFrame(0x00, pbData)

		var target wrapperspb.StringValue

		err := GRPCWebDecoder.Decode(bytes.NewReader(frame), &target)
		require.NoError(t, err)
		assert.Equal(t, "grpc_binary", target.GetValue())
	})

	t.Run("base64_text_frame_success", func(t *testing.T) {
		t.Parallel()

		pbData, _ := proto.Marshal(wrapperspb.String("grpc_base64"))
		frame := buildFrame(0x00, pbData)
		b64Payload := base64.StdEncoding.EncodeToString(frame)

		var target wrapperspb.StringValue

		err := GRPCWebDecoder.Decode(strings.NewReader(b64Payload), &target)
		require.NoError(t, err)
		assert.Equal(t, "grpc_base64", target.GetValue())
	})

	t.Run("gzip_compressed_frame_success", func(t *testing.T) {
		t.Parallel()

		pbData, _ := proto.Marshal(wrapperspb.String("grpc_gzip"))

		var gzBuf bytes.Buffer

		gzWriter := gzip.NewWriter(&gzBuf)
		_, err := gzWriter.Write(pbData)
		require.NoError(t, err)
		require.NoError(t, gzWriter.Close())

		frame := buildFrame(0x01, gzBuf.Bytes())

		var target wrapperspb.StringValue

		err = GRPCWebDecoder.Decode(bytes.NewReader(frame), &target)
		require.NoError(t, err)
		assert.Equal(t, "grpc_gzip", target.GetValue())
	})

	t.Run("trailer_frame_with_error_status", func(t *testing.T) {
		t.Parallel()

		trailer := []byte("grpc-status: 16\r\ngrpc-message: unauthenticated\r\n")
		frame := buildFrame(0x80, trailer)

		var target wrapperspb.StringValue

		err := GRPCWebDecoder.Decode(bytes.NewReader(frame), &target)
		assert.Error(t, err)

		var grpcErr *GRPCWebError
		require.ErrorAs(t, err, &grpcErr)
		assert.Equal(t, "16", grpcErr.StatusCode)
		assert.Equal(t, "unauthenticated", grpcErr.StatusMsg)
		assert.ErrorIs(t, err, ErrGRPCWebStatusError)
	})

	t.Run("trailer_frame_with_zero_status", func(t *testing.T) {
		t.Parallel()

		pbData, _ := proto.Marshal(wrapperspb.String("data"))
		dataFrame := buildFrame(0x00, pbData)
		trailerFrame := buildFrame(0x80, []byte("grpc-status: 0\r\n"))

		stream := bytes.NewReader(append(dataFrame, trailerFrame...))

		var target wrapperspb.StringValue

		err := GRPCWebDecoder.Decode(stream, &target)
		require.NoError(t, err)
		assert.Equal(t, "data", target.GetValue())
	})

	t.Run("corrupted_gzip_frame", func(t *testing.T) {
		t.Parallel()

		frame := buildFrame(0x01, []byte("invalid_gzip_bytes"))

		var target wrapperspb.StringValue

		err := GRPCWebDecoder.Decode(bytes.NewReader(frame), &target)
		assert.Error(t, err)
	})

	t.Run("truncated_header", func(t *testing.T) {
		t.Parallel()

		var target wrapperspb.StringValue

		err := GRPCWebDecoder.Decode(bytes.NewReader([]byte{0x00, 0x01}), &target)
		assert.Error(t, err)

		var grpcErr *GRPCWebError
		require.ErrorAs(t, err, &grpcErr)
		assert.Equal(t, "read_header", grpcErr.Op)
	})

	t.Run("truncated_payload", func(t *testing.T) {
		t.Parallel()

		header := make([]byte, 5)
		header[0] = 0x00
		binary.BigEndian.PutUint32(header[1:5], 100)

		var target wrapperspb.StringValue

		err := GRPCWebDecoder.Decode(bytes.NewReader(header), &target)
		assert.Error(t, err)

		var grpcErr *GRPCWebError
		require.ErrorAs(t, err, &grpcErr)
		assert.Equal(t, "read_payload", grpcErr.Op)
	})
}

func TestLimitDecoder(t *testing.T) {
	t.Parallel()

	ld := LimitDecoder(JSONDecoder, 10)

	type Data struct {
		A string `json:"a"`
	}

	var d Data

	err := ld.Decode(strings.NewReader(`{"a":"too_long_value"}`), &d)
	assert.Error(t, err)
}

func TestIsBase64Header(t *testing.T) {
	t.Parallel()

	assert.True(t, IsBase64Header([]byte("AAAAA=")))
	assert.False(t, IsBase64Header([]byte{0x00, 0x00, 0x00, 0x01, 0x00}))
	assert.False(t, IsBase64Header([]byte("abc")))
}

func TestStripBOM(t *testing.T) {
	t.Parallel()

	t.Run("utf8_bom", func(t *testing.T) {
		t.Parallel()

		input := []byte{0xEF, 0xBB, 0xBF, 'f', 'o', 'o'}
		r := StripBOM(bytes.NewReader(input))

		res, err := io.ReadAll(r)
		require.NoError(t, err)
		assert.Equal(t, "foo", string(res))
	})

	t.Run("utf16_be_bom", func(t *testing.T) {
		t.Parallel()

		input := []byte{0xFE, 0xFF, 'b', 'a', 'r'}
		r := StripBOM(bytes.NewReader(input))

		res, err := io.ReadAll(r)
		require.NoError(t, err)
		assert.Equal(t, "bar", string(res))
	})

	t.Run("utf16_le_bom", func(t *testing.T) {
		t.Parallel()

		input := []byte{0xFF, 0xFE, 'b', 'a', 'z'}
		r := StripBOM(bytes.NewReader(input))

		res, err := io.ReadAll(r)
		require.NoError(t, err)
		assert.Equal(t, "baz", string(res))
	})

	t.Run("no_bom", func(t *testing.T) {
		t.Parallel()

		input := []byte("plain_text")
		r := StripBOM(bytes.NewReader(input))

		res, err := io.ReadAll(r)
		require.NoError(t, err)
		assert.Equal(t, "plain_text", string(res))
	})
}

func TestTypeName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "<nil>", typeName(nil))
	assert.Equal(t, "int", typeName(42))
	assert.Equal(t, "*string", typeName(new(string)))
}

func TestGenericHelpersAndModifiers(t *testing.T) {
	t.Parallel()

	t.Run("proto_generic_helper", func(t *testing.T) {
		t.Parallel()

		data, _ := proto.Marshal(wrapperspb.String("generic_proto"))
		val, err := Proto[wrapperspb.StringValue](bytes.NewReader(data))
		require.NoError(t, err)
		assert.Equal(t, "generic_proto", val.GetValue())
	})

	t.Run("grpcweb_generic_helper", func(t *testing.T) {
		t.Parallel()

		pbData, _ := proto.Marshal(wrapperspb.String("generic_grpc"))
		buf := make([]byte, 5+len(pbData))
		buf[0] = 0x00
		binary.BigEndian.PutUint32(buf[1:5], uint32(len(pbData))) //nolint:gosec
		copy(buf[5:], pbData)

		val, err := GRPCWeb[wrapperspb.StringValue](bytes.NewReader(buf))
		require.NoError(t, err)
		assert.Equal(t, "generic_grpc", val.GetValue())
	})

	t.Run("modifiers_constructors", func(t *testing.T) {
		t.Parallel()

		assert.NotNil(t, WithProto())
		assert.NotNil(t, WithGRPCWeb())
		assert.NotNil(t, WithRaw())
		assert.NotNil(t, WithJSON())
		assert.NotNil(t, WithXML())
	})
}

func TestByContentType_JSONAndXMLAndFallback(t *testing.T) {
	t.Parallel()

	type Simple struct {
		Val string `json:"val" xml:"val"`
	}

	t.Run("application/json", func(t *testing.T) {
		t.Parallel()

		var s Simple

		err := ByContentType(strings.NewReader(`{"val":"json_ok"}`), "application/json; charset=utf-8", &s)
		require.NoError(t, err)
		assert.Equal(t, "json_ok", s.Val)
	})

	t.Run("application/xml", func(t *testing.T) {
		t.Parallel()

		var s Simple

		err := ByContentType(strings.NewReader(`<Simple><val>xml_ok</val></Simple>`), "application/xml", &s)
		require.NoError(t, err)
		assert.Equal(t, "xml_ok", s.Val)
	})

	t.Run("protobuf_content_type", func(t *testing.T) {
		t.Parallel()

		pbData, _ := proto.Marshal(wrapperspb.String("content_type_proto"))

		var target wrapperspb.StringValue

		err := ByContentType(bytes.NewReader(pbData), "application/x-protobuf", &target)
		require.NoError(t, err)
		assert.Equal(t, "content_type_proto", target.GetValue())
	})

	t.Run("grpc_web_content_type", func(t *testing.T) {
		t.Parallel()

		pbData, _ := proto.Marshal(wrapperspb.String("content_type_grpc"))
		buf := make([]byte, 5+len(pbData))
		buf[0] = 0x00
		binary.BigEndian.PutUint32(buf[1:5], uint32(len(pbData))) //nolint:gosec
		copy(buf[5:], pbData)

		var target wrapperspb.StringValue

		err := ByContentType(bytes.NewReader(buf), "application/grpc-web+proto", &target)
		require.NoError(t, err)
		assert.Equal(t, "content_type_grpc", target.GetValue())
	})

	t.Run("fallback_raw_decoder", func(t *testing.T) {
		t.Parallel()

		var raw []byte

		err := ByContentType(strings.NewReader("unknown_raw_bytes"), "application/unknown", &raw)
		require.NoError(t, err)
		assert.Equal(t, "unknown_raw_bytes", string(raw))
	})
}

type dummyCustomDecoder struct{}

func (dummyCustomDecoder) Decode(r io.Reader, target any) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if strPtr, ok := target.(*string); ok {
		*strPtr = "custom:" + string(data)
		return nil
	}

	return errors.New("invalid target")
}

func TestRegisterDecoder_Global(t *testing.T) {
	mime := "application/x-custom-test"
	dec := dummyCustomDecoder{}

	RegisterDecoder(mime, dec)
	defer UnregisterDecoder(mime)

	assert.Equal(t, dec, GetDecoder(mime))
	assert.Equal(t, dec, LookupDecoder(mime+"; charset=utf-8"))

	var target string

	err := ByContentType(strings.NewReader("hello"), mime, &target)
	require.NoError(t, err)
	assert.Equal(t, "custom:hello", target)

	UnregisterDecoder(mime)
	assert.Nil(t, GetDecoder(mime))
}
