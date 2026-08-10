// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// StatusCode represents official gRPC status codes (0-16) specified in PROTOCOL-HTTP2.md.
type StatusCode uint32

const (
	StatusOK                 StatusCode = 0
	StatusCancelled          StatusCode = 1
	StatusUnknown            StatusCode = 2
	StatusInvalidArgument    StatusCode = 3
	StatusDeadlineExceeded   StatusCode = 4
	StatusNotFound           StatusCode = 5
	StatusAlreadyExists      StatusCode = 6
	StatusPermissionDenied   StatusCode = 7
	StatusResourceExhausted  StatusCode = 8
	StatusFailedPrecondition StatusCode = 9
	StatusAborted            StatusCode = 10
	StatusOutOfRange         StatusCode = 11
	StatusUnimplemented      StatusCode = 12
	StatusInternal           StatusCode = 13
	StatusUnavailable        StatusCode = 14
	StatusDataLoss           StatusCode = 15
	StatusUnauthenticated    StatusCode = 16
)

// StatusError describes an RPC execution failure per PROTOCOL-HTTP2.md,
// including binary error details from the 'grpc-status-details-bin' trailer.
type StatusError struct {
	Code       StatusCode
	Message    string
	RawDetails []byte
	Header     http.Header
	Trailer    http.Header
}

func (e *StatusError) Error() string {
	if e == nil {
		return "<nil>"
	}

	if len(e.RawDetails) > 0 {
		return fmt.Sprintf(
			"aoni/grpc: status=%s (%d) msg=%s details_len=%d",
			e.Code.String(),
			e.Code,
			e.Message,
			len(e.RawDetails),
		)
	}

	return fmt.Sprintf("aoni/grpc: status=%s (%d) msg=%s", e.Code.String(), e.Code, e.Message)
}

func (c StatusCode) String() string {
	switch c {
	case StatusOK:
		return "OK"
	case StatusCancelled:
		return "CANCELLED"
	case StatusUnknown:
		return "UNKNOWN"
	case StatusInvalidArgument:
		return "INVALID_ARGUMENT"
	case StatusDeadlineExceeded:
		return "DEADLINE_EXCEEDED"
	case StatusNotFound:
		return "NOT_FOUND"
	case StatusAlreadyExists:
		return "ALREADY_EXISTS"
	case StatusPermissionDenied:
		return "PERMISSION_DENIED"
	case StatusResourceExhausted:
		return "RESOURCE_EXHAUSTED"
	case StatusFailedPrecondition:
		return "FAILED_PRECONDITION"
	case StatusAborted:
		return "ABORTED"
	case StatusOutOfRange:
		return "OUT_OF_RANGE"
	case StatusUnimplemented:
		return "UNIMPLEMENTED"
	case StatusInternal:
		return "INTERNAL"
	case StatusUnavailable:
		return "UNAVAILABLE"
	case StatusDataLoss:
		return "DATA_LOSS"
	case StatusUnauthenticated:
		return "UNAUTHENTICATED"
	default:
		return fmt.Sprintf("CODE_%d", c)
	}
}

func parseGRPCStatus(trailers http.Header) *StatusError {
	codeStr := trailers.Get("grpc-status")
	msgStr := trailers.Get("grpc-message")

	code, err := strconv.ParseUint(codeStr, 10, 32)
	if err != nil {
		code = uint64(StatusUnknown)
	}

	decodedMsg, err := url.QueryUnescape(msgStr)
	if err != nil {
		decodedMsg = msgStr
	}

	var rawDetails []byte
	if detailsBin := trailers.Get("grpc-status-details-bin"); detailsBin != "" {
		if decoded, err := DecodeBinaryHeader(detailsBin); err == nil {
			rawDetails = decoded
		}
	}

	return &StatusError{
		Code:       StatusCode(code),
		Message:    decodedMsg,
		RawDetails: rawDetails,
		Trailer:    trailers,
	}
}
