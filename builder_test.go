// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
)

func TestRequestBuilder_AuthProtocols(t *testing.T) {
	t.Run("BearerAuth replaced by BasicAuth without conflicting state", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHdr := r.Header.Get("Authorization")
			// Should only have Basic auth, Bearer should have been overwritten
			assert.True(t, authHdr != "")
			assert.Contains(t, authHdr, "Basic ")
			assert.NotContains(t, authHdr, "Bearer")

			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		client := aoni.New()
		resp, err := client.R().
			SetAuth(mod.WithBearer("legacy_token")).
			SetAuth(mod.WithBasicAuth("admin", "secret123")).
			Get(ts.URL)

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Custom Auth modifier", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			customHeader := r.Header.Get("X-Custom-Auth")
			assert.Equal(t, "my-secret-key", customHeader)

			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		client := aoni.New()
		resp, err := client.R().
			SetAuth(mod.WithHeader("X-Custom-Auth", "my-secret-key")).
			Get(ts.URL)

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestRequestBuilder_ConsumedGuard(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client := aoni.New()
	req := client.R()
	resp, err := req.Get(ts.URL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Second execution on consumed builder MUST return ErrBuilderConsumed
	_, err = req.Get(ts.URL)
	require.ErrorIs(t, err, aoni.ErrBuilderConsumed)

	// Acquiring a fresh builder from the client works as expected
	req2 := client.R()
	resp2, err := req2.Get(ts.URL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}

func TestRequestBuilder_BodyReplacement(t *testing.T) {
	t.Run("SetXMLBody replaces SetBody", func(t *testing.T) {
		type userXML struct {
			XMLName struct{} `xml:"user"`
			Name    string   `xml:"name"`
		}

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ct := r.Header.Get("Content-Type")
			assert.Equal(t, "application/xml", ct)

			bodyBytes, readErr := io.ReadAll(r.Body)
			require.NoError(t, readErr)
			assert.Equal(t, "<user><name>John</name></user>", string(bodyBytes))

			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		client := aoni.New()
		resp, err := client.R().
			SetBody("plain text body that should be replaced").
			SetXMLBody(userXML{Name: "John"}).
			Post(ts.URL)

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("SetProtoBody replaces previous body", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ct := r.Header.Get("Content-Type")
			assert.Equal(t, "application/x-protobuf", ct)

			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		client := aoni.New()
		msg := wrapperspb.String("hello protobuf")

		resp, err := client.R().
			SetBody(map[string]string{"foo": "bar"}).
			SetProtoBody(msg).
			Post(ts.URL)

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestRequestBuilder_ContextAndTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client := aoni.New()
	ctx := context.Background()

	resp, err := client.R().
		SetContext(ctx).
		SetHeader("X-Test", "val").
		Get(ts.URL)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRequestBuilder_PluginSystem(t *testing.T) {
	t.Run("Use BuilderPlugin", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "plugin-v1", r.Header.Get("X-Plugin-Header"))
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		testPlugin := aoni.BuilderPluginFunc(func(r *aoni.RequestBuilder) error {
			r.SetHeader("X-Plugin-Header", "plugin-v1")
			return nil
		})

		client := aoni.New()
		resp, err := client.R().
			Use(testPlugin).
			Get(ts.URL)

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("SetSigner applies late-binding signature", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sig := r.Header.Get("X-Signature")
			assert.Equal(t, "signed-hash-12345", sig)
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		signer := aoni.RequestSignerFunc(func(req *http.Request) error {
			req.Header.Set("X-Signature", "signed-hash-12345")
			return nil
		})

		client := aoni.New()
		resp, err := client.R().
			SetSigner(signer).
			Get(ts.URL)

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("SetSink consumes response", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("sink-payload-content"))
		}))
		defer ts.Close()

		var sinkContent string

		customSink := testSink{onConsume: func(resp *http.Response) error {
			b, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}

			sinkContent = string(b)

			return nil
		}}

		client := aoni.New()
		resp, err := client.R().
			SetSink(customSink).
			Get(ts.URL)

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "sink-payload-content", sinkContent)
	})

	t.Run("AddValidator intercepts invalid response", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Required-Status", "denied")
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		failValidator := aoni.ResponseValidatorFunc(func(resp *http.Response) error {
			if resp.Header.Get("X-Required-Status") == "denied" {
				return aoni.ErrUnexpectedStatus
			}

			return nil
		})

		client := aoni.New()
		_, err := client.R().
			AddValidator(failValidator).
			Get(ts.URL)

		require.Error(t, err)
		assert.ErrorIs(t, err, aoni.ErrUnexpectedStatus)
	})
}

type testSink struct {
	onConsume func(resp *http.Response) error
}

func (s testSink) ConsumeResponse(resp *http.Response) error {
	if s.onConsume != nil {
		return s.onConsume(resp)
	}

	return nil
}

func TestRequestBuilder_HeadersAndQueryParams(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "val1", r.Header.Get("X-H1"))
		assert.Equal(t, "val2", r.Header.Get("X-H2"))
		assert.Equal(t, "override", r.Header.Get("X-H3"))

		assert.Equal(t, "p1", r.URL.Query().Get("q1"))
		assert.Equal(t, "p2", r.URL.Query().Get("q2"))
		assert.Equal(t, "search", r.URL.Query().Get("term"))

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	type searchFilter struct {
		Term string `query:"term"`
	}

	client := aoni.New()
	resp, err := client.R().
		SetHeader("X-H1", "val1").
		SetHeaders(map[string]string{
			"X-H2": "val2",
			"X-H3": "override",
		}).
		SetQueryParam("q1", "p1").
		SetQueryParams(map[string]string{
			"q2": "p2",
		}).
		SetQueryStruct(searchFilter{Term: "search"}).
		Get(ts.URL)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRequestBuilder_PathParams(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/orgs/golang/repos/go/details", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client := aoni.New()
	resp, err := client.R().
		SetPathParam("org", "golang").
		SetPathParams(map[string]string{
			"repo":   "go",
			"action": "details",
		}).
		Get(ts.URL + "/orgs/{org}/repos/{repo}/{action}")

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRequestBuilder_FormData(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseMultipartForm(10 << 20)
		require.NoError(t, err)

		assert.Equal(t, "john", r.FormValue("username"))
		assert.Equal(t, "secret", r.FormValue("password"))

		file, _, err := r.FormFile("attachment")
		require.NoError(t, err)

		defer file.Close()

		content, err := io.ReadAll(file)
		require.NoError(t, err)
		assert.Equal(t, "file data content", string(content))

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client := aoni.New()
	resp, err := client.R().
		SetFormField("username", "john").
		SetFormField("password", "secret").
		SetFormFile("attachment", strings.NewReader("file data content")).
		Post(ts.URL)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRequestBuilder_ExpectStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer ts.Close()

	client := aoni.New()

	// Should fail when expected status doesn't match
	_, err := client.R().
		ExpectStatus(http.StatusOK, http.StatusCreated).
		Get(ts.URL)

	require.Error(t, err)
	assert.ErrorIs(t, err, aoni.ErrUnexpectedStatus)

	// Should succeed when expected status matches
	resp, err := client.R().
		ExpectStatus(http.StatusTeapot).
		Get(ts.URL)

	require.NoError(t, err)
	assert.Equal(t, http.StatusTeapot, resp.StatusCode)
}

func TestRequestBuilder_Results(t *testing.T) {
	t.Run("JSON result", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id": 123, "name": "Alice"}`))
		}))
		defer ts.Close()

		type userDTO struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}

		var res userDTO

		client := aoni.New()

		resp, err := client.R().
			SetResult(&res).
			Get(ts.URL)

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, 123, res.ID)
		assert.Equal(t, "Alice", res.Name)
	})

	t.Run("XML result via SetXMLResult", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<item><title>Go in Action</title></item>`))
		}))
		defer ts.Close()

		type itemXML struct {
			Title string `xml:"title"`
		}

		var res itemXML

		client := aoni.New()

		resp, err := client.R().
			SetXMLResult(&res).
			Get(ts.URL)

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "Go in Action", res.Title)
	})
}
