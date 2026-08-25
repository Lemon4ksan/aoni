// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dynamic_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/grpc"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/x/grpc/dynamic"
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

	invoker := dynamic.New()
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

	invoker := dynamic.New()
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
