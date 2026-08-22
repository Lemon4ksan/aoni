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

func TestASTGenericFindAndPredicates(t *testing.T) {
	file := ast.NewFile("shop")
	svc := file.NewService("ShopService")
	svc.NewMethod("GetProduct", "GET", "products/{id}")

	st := file.NewStruct("Product")
	st.AddField("ID", "string", "id", true)

	if !svc.HasMethods() {
		t.Errorf("expected service to have methods")
	}

	if !st.HasFields() {
		t.Errorf("expected struct to have fields")
	}

	foundSvc, ok := file.FindService("ShopService")
	if !ok || foundSvc == nil || foundSvc.Name != "ShopService" {
		t.Fatalf("expected to find ShopService")
	}

	foundSvcOpt, okOpt := file.FindServiceOptional("ShopService").Value()
	if !okOpt || foundSvcOpt == nil || foundSvcOpt.Name != "ShopService" {
		t.Fatalf("expected to find ShopService via Optional")
	}

	foundMethod, ok := foundSvc.FindMethod("GetProduct")
	if !ok || foundMethod == nil || foundMethod.Name != "GetProduct" {
		t.Fatalf("expected to find GetProduct method")
	}

	foundMethodOpt, okOpt := foundSvc.FindMethodOptional("GetProduct").Value()
	if !okOpt || foundMethodOpt == nil || foundMethodOpt.Name != "GetProduct" {
		t.Fatalf("expected to find GetProduct method via Optional")
	}

	foundStruct, ok := file.FindStruct("Product")
	if !ok || foundStruct == nil || foundStruct.Name != "Product" {
		t.Fatalf("expected to find Product struct")
	}

	foundStructOpt, okOpt := file.FindStructOptional("Product").Value()
	if !okOpt || foundStructOpt == nil || foundStructOpt.Name != "Product" {
		t.Fatalf("expected to find Product struct via Optional")
	}

	foundField, ok := foundStruct.FindField("ID")
	if !ok || foundField == nil || foundField.Name != "ID" {
		t.Fatalf("expected to find ID field")
	}

	foundFieldOpt, okOpt := foundStruct.FindFieldOptional("ID").Value()
	if !okOpt || foundFieldOpt == nil || foundFieldOpt.Name != "ID" {
		t.Fatalf("expected to find ID field via Optional")
	}

	// Tuples, Unions, Bitpacks
	file.Tuples = append(file.Tuples, &ast.Tuple{Name: "Point"})
	file.Unions = append(file.Unions, &ast.Union{Name: "Shape"})
	file.Bitpacks = append(file.Bitpacks, &ast.Bitpack{Name: "Flags"})

	if _, ok := file.FindTuple("Point"); !ok {
		t.Errorf("expected to find Point tuple")
	}

	if opt := file.FindTupleOptional("Point"); !opt.IsPresent() {
		t.Errorf("expected to find Point tuple via Optional")
	}

	if _, ok := file.FindUnion("Shape"); !ok {
		t.Errorf("expected to find Shape union")
	}

	if opt := file.FindUnionOptional("Shape"); !opt.IsPresent() {
		t.Errorf("expected to find Shape union via Optional")
	}

	if _, ok := file.FindBitpack("Flags"); !ok {
		t.Errorf("expected to find Flags bitpack")
	}

	if opt := file.FindBitpackOptional("Flags"); !opt.IsPresent() {
		t.Errorf("expected to find Flags bitpack via Optional")
	}

	if _, ok := file.FindService("NonExistent"); ok {
		t.Errorf("expected false for non-existent service")
	}

	if _, ok := file.FindServiceOptional("NonExistent").Value(); ok {
		t.Errorf("expected None for non-existent service optional")
	}
}
