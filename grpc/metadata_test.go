// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/grpc"
	"github.com/lemon4ksan/aoni/option"
)

func TestMetadata_ContextAndModifiers(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer secret-token", r.Header.Get("authorization"))
		assert.NotEmpty(t, r.Header.Get("trace-id-bin"))
		assert.Equal(t, "5S", r.Header.Get("grpc-timeout"))

		respMsg := wrapperspb.String("ok")
		frame, err := grpc.MarshalFrame(respMsg, false)
		require.NoError(t, err)

		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("grpc-status", "0")
		w.WriteHeader(http.StatusOK)

		_, _ = w.Write(frame)
	}))
	defer ts.Close()

	client := aoni.NewClient(option.WithBaseURL(ts.URL))

	md := grpc.Metadata{
		"authorization": "Bearer secret-token",
		"trace-id-bin":  "trace-12345",
	}

	ctx := grpc.NewContext(context.Background(), md)

	retrievedMD, ok := grpc.FromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, "Bearer secret-token", retrievedMD["authorization"])

	resp, err := grpc.Invoke[wrapperspb.StringValue](
		ctx,
		client,
		ts.URL+"/TestService/TestMethod",
		wrapperspb.String("request"),
		grpc.WithTimeout(5*time.Second),
		grpc.WithBinaryHeader("custom-bin", []byte{0x01, 0x02}),
	)

	require.NoError(t, err)
	assert.Equal(t, "ok", resp.GetValue())
}
