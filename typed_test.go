// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/grpc"
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

	client := aoni.NewClient(nil, aoni.WithBaseURL(ts.URL))

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

	client := aoni.NewClient(nil, aoni.WithBaseURL(ts.URL))

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

	client := aoni.NewClient(nil, aoni.WithBaseURL(ts.URL))

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

	client := aoni.NewClient(nil, aoni.WithBaseURL(ts.URL))
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

	client := aoni.NewClient(nil, aoni.WithBaseURL(ts.URL))

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

	client := aoni.NewClient(nil, aoni.WithBaseURL(ts.URL))

	resp, err := client.Raw().Get(context.Background(), "/raw")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
}

func TestClientGRPC(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/grpc" {
			t.Fatalf("expected application/grpc, got %s", r.Header.Get("Content-Type"))
		}

		resMsg := wrapperspb.String("Hello from native gRPC over aoni!")

		frameBytes, err := grpc.MarshalFrame(resMsg, false)
		if err != nil {
			t.Fatalf("failed to marshal gRPC frame: %v", err)
		}

		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("grpc-status", "0")
		_, _ = w.Write(frameBytes)
	}))
	defer ts.Close()

	client := aoni.NewClient(nil, aoni.WithBaseURL(ts.URL))

	reqMsg := wrapperspb.String("Ping")

	res, err := client.GRPC().Invoke[wrapperspb.StringValue](
		context.Background(),
		"/test.TestService/Ping",
		reqMsg,
	)
	if err != nil {
		t.Fatalf("gRPC invoke failed: %v", err)
	}

	if res.GetValue() != "Hello from native gRPC over aoni!" {
		t.Fatalf("unexpected gRPC response: %s", res.GetValue())
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
	fastClient := fast.NewClient(aoni.WithBaseURL(ts.URL))

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
	c := aoni.NewClient(nil, aoni.WithBaseURL(ts.URL))

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

