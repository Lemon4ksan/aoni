// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/grpc"
	"github.com/lemon4ksan/aoni/option"
)

func TestDynamicInvoker_InvokeJSON(t *testing.T) {
	t.Parallel()

	desc := (&wrapperspb.StringValue{}).ProtoReflect().Descriptor()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/grpc", r.Header.Get("Content-Type"))
		assert.Equal(t, "trailers", r.Header.Get("TE"))

		respMsg := wrapperspb.String("server dynamic response")
		frame, err := grpc.MarshalFrame(respMsg, false)
		require.NoError(t, err)

		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("grpc-status", "0")
		w.WriteHeader(http.StatusOK)

		_, _ = w.Write(frame)
	}))
	defer ts.Close()

	invoker := grpc.NewDynamicInvoker()
	client := aoni.NewClient(option.WithBaseURL(ts.URL))

	jsonResp, err := invoker.InvokeJSON(
		context.Background(),
		client,
		ts.URL+"/TestService/TestMethod",
		`"client dynamic request"`,
		desc,
		desc,
	)

	require.NoError(t, err)
	assert.Contains(t, jsonResp, "server dynamic response")
}

func TestDynamicInvoker_NilDescriptors(t *testing.T) {
	t.Parallel()

	invoker := grpc.NewDynamicInvoker()
	_, err := invoker.InvokeJSON(
		context.Background(),
		nil,
		"/TestService/TestMethod",
		`{}`,
		nil,
		nil,
	)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MessageDescriptors must not be nil")
}

func TestDynamicInvoker_StatusErrorHandling(t *testing.T) {
	t.Parallel()

	desc := (&wrapperspb.StringValue{}).ProtoReflect().Descriptor()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("grpc-status", "7") // Permission Denied
		w.Header().Set("grpc-message", "Permission%20Denied%20Error")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	invoker := grpc.NewDynamicInvoker()
	client := aoni.NewClient(option.WithBaseURL(ts.URL))

	_, err := invoker.InvokeJSON(
		context.Background(),
		client,
		ts.URL+"/TestService/TestMethod",
		`"client request"`,
		desc,
		desc,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PERMISSION_DENIED")
	assert.Contains(t, err.Error(), "Permission Denied Error")
}

func TestDynamicInvoker_InvalidJSONInput(t *testing.T) {
	t.Parallel()

	desc := (&wrapperspb.StringValue{}).ProtoReflect().Descriptor()
	invoker := grpc.NewDynamicInvoker()

	_, err := invoker.InvokeJSON(
		context.Background(),
		nil,
		"/TestService/TestMethod",
		`{invalid json`,
		desc,
		desc,
	)

	assert.Error(t, err)
}
