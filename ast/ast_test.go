// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ast_test

import (
	"strings"
	"testing"

	"github.com/lemon4ksan/aoni/ast"
)

func TestASTBuilderAndPrinter(t *testing.T) {
	file := ast.NewFile("telegram")

	svc := file.NewService("TelegramBotAPI").
		WithBaseURL("https://api.telegram.org/bot${BOT_TOKEN}").
		WithEngine(ast.EngineFast).
		WithDoc("TelegramBotAPI provides type-safe client bindings for Telegram Bot API.")

	svc.NewMethod("SendMessage", "POST", "sendMessage").
		WithDoc("SendMessage sends text messages.").
		WithRequest("SendMessageRequest").
		WithResponse("*Message")

	svc.NewMethod("GetMe", "GET", "getMe").
		WithDoc("GetMe tests bot authentication.").
		WithResponse("*User")

	msgReq := file.NewStruct("SendMessageRequest").
		WithDoc("SendMessageRequest parameters.")

	msgReq.AddField("ChatID", "int64", "chat_id", true)
	msgReq.AddField("Text", "string", "text", true)
	msgReq.AddField("ParseMode", "*string", "parse_mode", false)

	userStruct := file.NewStruct("User").
		WithDoc("User represents a Telegram user or bot.")

	userStruct.AddField("ID", "int64", "id", true)
	userStruct.AddField("IsBot", "bool", "is_bot", true)
	userStruct.AddField("FirstName", "string", "first_name", true)
	userStruct.AddField("Username", "*string", "username", false)

	code, err := ast.Format(file)
	if err != nil {
		t.Fatalf("ast.Format failed: %v", err)
	}

	src := string(code)
	if !strings.Contains(src, "package telegram") {
		t.Errorf("expected package telegram, got:\n%s", src)
	}

	if !strings.Contains(src, "@aoni:service casing=snake_case") {
		t.Errorf("expected @aoni:service tag, got:\n%s", src)
	}

	if !strings.Contains(
		src,
		"SendMessage(ctx context.Context, req *SendMessageRequest, mods ...aoni.RequestModifier) (*Message, error)",
	) {
		t.Errorf("expected SendMessage signature, got:\n%s", src)
	}

	if !strings.Contains(src, "GetMe(ctx context.Context, mods ...aoni.RequestModifier) (*User, error)") {
		t.Errorf("expected GetMe signature, got:\n%s", src)
	}

	if !strings.Contains(src, "`json:\"chat_id\"`") {
		t.Errorf("expected required field json tag, got:\n%s", src)
	}

	if !strings.Contains(src, "`json:\"parse_mode,omitempty\"`") {
		t.Errorf("expected omitempty field json tag, got:\n%s", src)
	}
}

func TestASTMultilineWrapping(t *testing.T) {
	file := ast.NewFile("api").WithMaxLen(80)

	svc := file.NewService("API")
	svc.NewMethod("SuperLongMethodNameWithManyArguments", "POST", "longEndpoint").
		WithRequest("VeryLongDescriptiveRequestModelName").
		WithResponse("*VeryLongDescriptiveResponseModelName")

	code, err := ast.Format(file)
	if err != nil {
		t.Fatalf("ast.Format failed: %v", err)
	}

	src := string(code)
	expectedMultiline := `	SuperLongMethodNameWithManyArguments(
		ctx context.Context,
		req *VeryLongDescriptiveRequestModelName,
		mods ...aoni.RequestModifier,
	) (*VeryLongDescriptiveResponseModelName, error)`

	if !strings.Contains(src, expectedMultiline) {
		t.Errorf("expected multiline wrapped method, got:\n%s", src)
	}
}
