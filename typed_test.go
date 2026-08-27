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
		_ = json.NewEncoder(w).Encode(UserDTO{ID: 55, Name: "Fast Gordon"})
	}))
	defer ts.Close()

	fastClient := fast.NewClient(aoni.WithBaseURL(ts.URL))

	user, err := fastClient.GetTo[UserDTO](context.Background(), "/users/55")
	if err != nil {
		t.Fatalf("fast client get failed: %v", err)
	}

	if user.ID != 55 || user.Name != "Fast Gordon" {
		t.Fatalf("unexpected fast user: %+v", user)
	}
}
