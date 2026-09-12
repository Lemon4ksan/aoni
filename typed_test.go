// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

type UserDTO struct {
	ID    uint64 `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func TestClientTypedGet(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}

		if r.URL.Path != "/users/42" {
			t.Fatalf("expected /users/42, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(UserDTO{ID: 42, Name: "Gordon", Email: "gordon@blackmesa.gov"})
	}))
	defer ts.Close()

	client := aoni.NewClient(nil, option.WithBaseURL(ts.URL))

	ctx := context.Background()

	user, err := client.GetTo[UserDTO](ctx, "/users/42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if user.ID != 42 || user.Name != "Gordon" || user.Email != "gordon@blackmesa.gov" {
		t.Fatalf("unexpected user DTO: %+v", user)
	}
}

func TestClientTypedGetInto(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(UserDTO{ID: 99, Name: "Alyx", Email: "alyx@vance.com"})
	}))
	defer ts.Close()

	client := aoni.NewClient(nil, option.WithBaseURL(ts.URL))

	var user UserDTO

	err := client.GetInto(context.Background(), "/users/99", &user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if user.ID != 99 || user.Name != "Alyx" {
		t.Fatalf("unexpected user DTO: %+v", user)
	}
}

func TestClientTypedPost(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		var req CreateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(UserDTO{ID: 100, Name: req.Name, Email: req.Email})
	}))
	defer ts.Close()

	client := aoni.NewClient(nil, option.WithBaseURL(ts.URL))

	reqPayload := CreateUserRequest{Name: "Eli", Email: "eli@blackmesa.gov"}

	user, err := client.PostTo[UserDTO](context.Background(), "/users", reqPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if user.ID != 100 || user.Name != "Eli" {
		t.Fatalf("unexpected user response: %+v", user)
	}
}

func TestClientTypedPutPatchDelete(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case http.MethodPut:
			_ = json.NewEncoder(w).Encode(UserDTO{ID: 1, Name: "Barney Put"})
		case http.MethodPatch:
			_ = json.NewEncoder(w).Encode(UserDTO{ID: 1, Name: "Barney Patch"})
		case http.MethodDelete:
			_ = json.NewEncoder(w).Encode(UserDTO{ID: 1, Name: "Barney Deleted"})
		default:
			http.Error(w, "invalid method", http.StatusMethodNotAllowed)
		}
	}))
	defer ts.Close()

	client := aoni.NewClient(nil, option.WithBaseURL(ts.URL))
	ctx := context.Background()

	// Put
	putUser, err := client.PutTo[UserDTO](ctx, "/users/1", CreateUserRequest{Name: "Barney Put"})
	if err != nil || putUser.Name != "Barney Put" {
		t.Fatalf("Put failed: %v, user: %+v", err, putUser)
	}

	// Patch
	patchUser, err := client.PatchTo[UserDTO](ctx, "/users/1", CreateUserRequest{Name: "Barney Patch"})
	if err != nil || patchUser.Name != "Barney Patch" {
		t.Fatalf("Patch failed: %v, user: %+v", err, patchUser)
	}

	// Delete
	delUser, err := client.DeleteTo[UserDTO](ctx, "/users/1")
	if err != nil || delUser.Name != "Barney Deleted" {
		t.Fatalf("Delete failed: %v, user: %+v", err, delUser)
	}
}

func TestClientTypedEx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "aoni-v1")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(UserDTO{ID: 7, Name: "Seven"})
	}))
	defer ts.Close()

	client := aoni.NewClient(nil, option.WithBaseURL(ts.URL))

	user, rawResp, err := client.PostEx[UserDTO](context.Background(), "/users", CreateUserRequest{Name: "Seven"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer rawResp.Body.Close()

	if rawResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", rawResp.StatusCode)
	}

	if rawResp.Header.Get("X-Custom-Header") != "aoni-v1" {
		t.Fatalf("expected X-Custom-Header header, got %s", rawResp.Header.Get("X-Custom-Header"))
	}

	if user.ID != 7 || user.Name != "Seven" {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestClientRaw(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("raw string payload"))
	}))
	defer ts.Close()

	client := aoni.NewClient(nil, option.WithBaseURL(ts.URL))

	resp, err := client.Get(context.Background(), "/raw")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
}

func TestFastClientTyped(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(UserDTO{ID: 55, Name: "Fast Gordon"})
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(UserDTO{ID: 56, Name: "Fast Post"})
		case http.MethodPut:
			_ = json.NewEncoder(w).Encode(UserDTO{ID: 57, Name: "Fast Put"})
		case http.MethodPatch:
			_ = json.NewEncoder(w).Encode(UserDTO{ID: 58, Name: "Fast Patch"})
		case http.MethodDelete:
			_ = json.NewEncoder(w).Encode(UserDTO{ID: 59, Name: "Fast Delete"})
		default:
			_ = json.NewEncoder(w).Encode(UserDTO{ID: 60, Name: "Fast Other"})
		}
	}))
	defer ts.Close()

	ctx := context.Background()
	fastClient := fast.NewClient(option.WithBaseURL(ts.URL))

	// GetTo & GetInto
	user, err := fastClient.GetTo[UserDTO](ctx, "/users/55")
	if err != nil || user.ID != 55 {
		t.Fatalf("fast get failed: %v", err)
	}

	var userInto UserDTO

	err = fastClient.GetInto(ctx, "/users/55", &userInto)
	if err != nil || userInto.ID != 55 {
		t.Fatalf("fast get into failed: %v", err)
	}

	// PostTo & PostInto
	pUser, err := fastClient.PostTo[UserDTO](ctx, "/users", CreateUserRequest{Name: "Post"})
	if err != nil || pUser.ID != 56 {
		t.Fatalf("fast post failed: %v", err)
	}

	err = fastClient.PostInto(ctx, "/users", CreateUserRequest{Name: "Post"}, &userInto)
	if err != nil || userInto.ID != 56 {
		t.Fatalf("fast post into failed: %v", err)
	}

	// PutTo, PatchTo, DeleteTo, FetchTo
	putUser, err := fastClient.PutTo[UserDTO](ctx, "/users/57", CreateUserRequest{Name: "Put"})
	if err != nil || putUser.ID != 57 {
		t.Fatalf("fast put failed: %v", err)
	}

	patchUser, err := fastClient.PatchTo[UserDTO](ctx, "/users/58", CreateUserRequest{Name: "Patch"})
	if err != nil || patchUser.ID != 58 {
		t.Fatalf("fast patch failed: %v", err)
	}

	delUser, err := fastClient.DeleteTo[UserDTO](ctx, "/users/59")
	if err != nil || delUser.ID != 59 {
		t.Fatalf("fast delete failed: %v", err)
	}

	fetchUser, err := fastClient.FetchTo[UserDTO](ctx, "GET", "/users/55", nil)
	if err != nil || fetchUser.ID != 55 {
		t.Fatalf("fast fetch failed: %v", err)
	}

	// Raw response methods on fastClient
	rGet, err := fastClient.Get(ctx, "/users/55")
	if err != nil || rGet.StatusCode() != http.StatusOK {
		t.Fatalf("fast raw get failed: %v", err)
	}

	rPost, err := fastClient.Post(ctx, "/users", "body")
	if err != nil || rPost.StatusCode() != http.StatusOK {
		t.Fatalf("fast raw post failed: %v", err)
	}

	rPut, err := fastClient.Put(ctx, "/users/57", "body")
	if err != nil || rPut.StatusCode() != http.StatusOK {
		t.Fatalf("fast raw put failed: %v", err)
	}

	rPatch, err := fastClient.Patch(ctx, "/users/58", "body")
	if err != nil || rPatch.StatusCode() != http.StatusOK {
		t.Fatalf("fast raw patch failed: %v", err)
	}

	rDel, err := fastClient.Delete(ctx, "/users/59")
	if err != nil || rDel.StatusCode() != http.StatusOK {
		t.Fatalf("fast raw delete failed: %v", err)
	}

	rFetch, err := fastClient.Fetch(ctx, "GET", "/users/60", nil)
	if err != nil || rFetch.StatusCode() != http.StatusOK {
		t.Fatalf("fast raw fetch failed: %v", err)
	}
}

func TestStandardClient_AllMethods_And_PackageLevel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(UserDTO{ID: 1, Name: "Get"})
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(UserDTO{ID: 2, Name: "Post"})
		case http.MethodPut:
			_ = json.NewEncoder(w).Encode(UserDTO{ID: 3, Name: "Put"})
		case http.MethodPatch:
			_ = json.NewEncoder(w).Encode(UserDTO{ID: 4, Name: "Patch"})
		case http.MethodDelete:
			_ = json.NewEncoder(w).Encode(UserDTO{ID: 5, Name: "Delete"})
		case http.MethodOptions:
			w.WriteHeader(http.StatusNoContent)
		default:
			_ = json.NewEncoder(w).Encode(UserDTO{ID: 6, Name: "Fetch"})
		}
	}))
	defer ts.Close()

	ctx := context.Background()
	c := aoni.NewClient(nil, option.WithBaseURL(ts.URL))

	// PutTo, PatchTo, DeleteTo, FetchTo
	uPut, err := c.PutTo[UserDTO](ctx, "/items/3", UserDTO{ID: 3})
	if err != nil || uPut.ID != 3 {
		t.Fatalf("put failed: %v", err)
	}

	uPatch, err := c.PatchTo[UserDTO](ctx, "/items/4", UserDTO{ID: 4})
	if err != nil || uPatch.ID != 4 {
		t.Fatalf("patch failed: %v", err)
	}

	uDel, err := c.DeleteTo[UserDTO](ctx, "/items/5")
	if err != nil || uDel.ID != 5 {
		t.Fatalf("delete failed: %v", err)
	}

	uFetch, err := c.FetchTo[UserDTO](ctx, "GET", "/items/1", nil)
	if err != nil || uFetch.ID != 1 {
		t.Fatalf("fetch failed: %v", err)
	}

	// Raw response methods
	rGet, err := c.Get(ctx, "/items/1")
	if err != nil || rGet.StatusCode != http.StatusOK {
		t.Fatalf("raw get failed: %v", err)
	}

	rPost, err := c.Post(ctx, "/items", "data")
	if err != nil || rPost.StatusCode != http.StatusOK {
		t.Fatalf("raw post failed: %v", err)
	}

	rPut, err := c.Put(ctx, "/items/3", "data")
	if err != nil || rPut.StatusCode != http.StatusOK {
		t.Fatalf("raw put failed: %v", err)
	}

	rPatch, err := c.Patch(ctx, "/items/4", "data")
	if err != nil || rPatch.StatusCode != http.StatusOK {
		t.Fatalf("raw patch failed: %v", err)
	}

	rDel, err := c.Delete(ctx, "/items/5")
	if err != nil || rDel.StatusCode != http.StatusOK {
		t.Fatalf("raw delete failed: %v", err)
	}

	rOpt, err := c.Options(ctx, "/items")
	if err != nil || rOpt.StatusCode != http.StatusNoContent {
		t.Fatalf("raw options failed: %v", err)
	}

	rFetch, err := c.Fetch(ctx, "GET", "/items/1", nil)
	if err != nil || rFetch.StatusCode != http.StatusOK {
		t.Fatalf("raw fetch failed: %v", err)
	}

	// Package-level calls
	pkgUser, err := aoni.GetTo[UserDTO](ctx, ts.URL+"/items/1")
	if err != nil || pkgUser.ID != 1 {
		t.Fatalf("pkg get failed: %v", err)
	}

	pkgPostUser, err := aoni.PostTo[UserDTO](ctx, ts.URL+"/items", UserDTO{ID: 2})
	if err != nil || pkgPostUser.ID != 2 {
		t.Fatalf("pkg post failed: %v", err)
	}

	pkgPutUser, err := aoni.PutTo[UserDTO](ctx, ts.URL+"/items/3", UserDTO{ID: 3})
	if err != nil || pkgPutUser.ID != 3 {
		t.Fatalf("pkg put failed: %v", err)
	}

	pkgPatchUser, err := aoni.PatchTo[UserDTO](ctx, ts.URL+"/items/4", UserDTO{ID: 4})
	if err != nil || pkgPatchUser.ID != 4 {
		t.Fatalf("pkg patch failed: %v", err)
	}

	pkgDelUser, err := aoni.DeleteTo[UserDTO](ctx, ts.URL+"/items/5")
	if err != nil || pkgDelUser.ID != 5 {
		t.Fatalf("pkg delete failed: %v", err)
	}
}

type sampleUser struct {
	ID    int    `json:"id"`
	Email string `json:"email"`
}

func newTypedEchoServer(t *testing.T) *httptest.Server {
	t.Helper()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)

		if r.URL.Path == "/html-error" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<!DOCTYPE html><html><body>Error Page</body></html>"))

			return
		}

		if r.URL.Path == "/api-error" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":400,"message":"bad parameters"}`))

			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Echo-Method", r.Method)
		w.Header().Set("X-Echo-Content-Type", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)

		if len(bodyBytes) > 0 {
			_, _ = w.Write(bodyBytes)
		} else {
			_, _ = w.Write([]byte(`{"id":1,"email":"test@example.com"}`))
		}
	}))
	t.Cleanup(ts.Close)

	return ts
}

func TestTyped_ExMethods(t *testing.T) {
	t.Parallel()

	ts := newTypedEchoServer(t)
	client := aoni.NewClient(option.WithBaseURL(ts.URL))

	t.Run("get_ex", func(t *testing.T) {
		user, resp, err := client.GetEx[sampleUser](t.Context(), "/get")
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, 1, user.ID)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, http.MethodGet, resp.Header.Get("X-Echo-Method"))
	})

	t.Run("post_ex", func(t *testing.T) {
		reqUser := sampleUser{ID: 2, Email: "post@example.com"}
		user, resp, err := client.PostEx[sampleUser](t.Context(), "/post", reqUser)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, 2, user.ID)
		assert.Equal(t, "post@example.com", user.Email)
		assert.Equal(t, http.MethodPost, resp.Header.Get("X-Echo-Method"))
	})

	t.Run("put_ex", func(t *testing.T) {
		reqUser := sampleUser{ID: 3, Email: "put@example.com"}
		user, resp, err := client.PutEx[sampleUser](t.Context(), "/put", reqUser)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, 3, user.ID)
		assert.Equal(t, http.MethodPut, resp.Header.Get("X-Echo-Method"))
	})

	t.Run("patch_ex", func(t *testing.T) {
		reqUser := sampleUser{ID: 4, Email: "patch@example.com"}
		user, resp, err := client.PatchEx[sampleUser](t.Context(), "/patch", reqUser)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, 4, user.ID)
		assert.Equal(t, http.MethodPatch, resp.Header.Get("X-Echo-Method"))
	})

	t.Run("delete_to_and_delete_ex", func(t *testing.T) {
		user, err := client.DeleteTo[sampleUser](t.Context(), "/delete")
		require.NoError(t, err)
		assert.Equal(t, 1, user.ID)

		userEx, resp, err := client.DeleteEx[sampleUser](t.Context(), "/delete-ex")
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, 1, userEx.ID)
		assert.Equal(t, http.MethodDelete, resp.Header.Get("X-Echo-Method"))
	})

	t.Run("fetch_ex_and_do_ex", func(t *testing.T) {
		user, resp, err := client.FetchEx[sampleUser](t.Context(), http.MethodOptions, "/fetch-ex", nil)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, 1, user.ID)
		assert.Equal(t, http.MethodOptions, resp.Header.Get("X-Echo-Method"))

		userDo, respDo, err := client.DoEx[sampleUser](t.Context(), http.MethodOptions, "/do-ex", nil)
		require.NoError(t, err)

		defer respDo.Body.Close()

		assert.Equal(t, 1, userDo.ID)
	})
}

func TestTyped_PayloadTypes(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Echo-Content-Type", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)

		respMap := map[string]any{
			"contentType": r.Header.Get("Content-Type"),
			"body":        string(body),
		}
		_ = json.NewEncoder(w).Encode(respMap)
	}))
	t.Cleanup(ts.Close)

	client := aoni.NewClient(option.WithBaseURL(ts.URL))

	type echoMeta struct {
		ContentType string `json:"contentType"`
		Body        string `json:"body"`
	}

	tests := []struct {
		name            string
		payload         any
		wantContentType string
		bodySubstring   string
	}{
		{
			name:            "url_values_payload",
			payload:         url.Values{"user": {"alice"}, "role": {"admin"}},
			wantContentType: "application/x-www-form-urlencoded",
			bodySubstring:   "user=alice",
		},
		{
			name:            "url_values_pointer_payload",
			payload:         &url.Values{"scope": {"read", "write"}},
			wantContentType: "application/x-www-form-urlencoded",
			bodySubstring:   "scope=read",
		},
		{
			name:            "string_payload",
			payload:         "raw-string-content",
			wantContentType: "",
			bodySubstring:   "raw-string-content",
		},
		{
			name:            "bytes_payload",
			payload:         []byte("raw-bytes-content"),
			wantContentType: "",
			bodySubstring:   "raw-bytes-content",
		},
		{
			name:            "io_reader_payload",
			payload:         strings.NewReader("streamed-body-data"),
			wantContentType: "",
			bodySubstring:   "streamed-body-data",
		},
		{
			name:            "json_struct_payload",
			payload:         sampleUser{ID: 42, Email: "struct@test.com"},
			wantContentType: "application/json",
			bodySubstring:   `"id":42`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := client.PostTo[echoMeta](t.Context(), "/echo", tt.payload)
			require.NoError(t, err)
			assert.Equal(t, tt.wantContentType, meta.ContentType)
			assert.Contains(t, meta.Body, tt.bodySubstring)
		})
	}

	t.Run("modifier_as_body_returns_error", func(t *testing.T) {
		t.Parallel()
		_, err := client.PostTo[sampleUser](t.Context(), "/echo", mod.WithHeader("X-Bad", "1"))
		assert.ErrorIs(t, err, aoni.ErrModifierAsBody)
	})
}

func TestTyped_HTMLDetectionAndPeekableReader(t *testing.T) {
	t.Parallel()

	ts := newTypedEchoServer(t)
	client := aoni.NewClient(option.WithBaseURL(ts.URL))

	t.Run("html_error_response_detected", func(t *testing.T) {
		t.Parallel()
		_, err := client.GetTo[sampleUser](t.Context(), "/html-error")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected structured data but got HTML")
	})

	t.Run("peekable_reader_resolution", func(t *testing.T) {
		t.Parallel()
		resp, err := client.Get(t.Context(), "/get")
		require.NoError(t, err)

		defer resp.Body.Close()

		reader1 := aoni.ResolvePeekableReader(resp)
		require.NotNil(t, reader1)

		peekBytes, err := reader1.Peek(5)
		require.NoError(t, err)
		assert.NotEmpty(t, peekBytes)

		reader2 := aoni.ResolvePeekableReader(resp)
		assert.Equal(t, reader1, reader2)
	})
}

func TestTyped_DebugDiagnostics(t *testing.T) {
	t.Parallel()

	ts := newTypedEchoServer(t)
	client := aoni.NewClient(option.WithBaseURL(ts.URL))

	t.Run("debug_request_with_sensitive_headers", func(t *testing.T) {
		t.Parallel()
		user, err := client.PostTo[sampleUser](t.Context(), "/post",
			sampleUser{ID: 99, Email: "debug@test.com"},
			mod.WithDebug(),
			mod.WithHeader("Authorization", "Bearer secret-999"),
			mod.WithHeader("Cookie", "session=top-secret"),
		)
		require.NoError(t, err)
		assert.Equal(t, 99, user.ID)
	})
}
