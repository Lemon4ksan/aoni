// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package spec provides the definitive, self-documenting registry of all DSL directives,
// arguments, scopes, pipeline stages, and validation rules for aoni-gen.
package spec

import (
	"fmt"
	"sort"
	"strings"
)

// Scope categorizes the declaration level where a directive or stage is valid.
type Scope string

const (
	// ScopeService applies to interface type declarations.
	ScopeService Scope = "service"

	// ScopeSocket applies to persistent real-time socket interfaces.
	ScopeSocket Scope = "socket"

	// ScopeMethod applies to interface method signatures.
	ScopeMethod Scope = "method"

	// ScopeParam applies to function parameters within method signatures.
	ScopeParam Scope = "param"

	// ScopeStruct applies to DTO structs, tuples, and unions.
	ScopeStruct Scope = "struct"

	// ScopePipeline applies to Wire-Transform pipeline stages in @return / @body expressions.
	ScopePipeline Scope = "pipeline"
)

// ArgDef describes an argument or flag accepted by a directive or pipeline stage.
type ArgDef struct {
	Name          string   `json:"name"`
	Placeholder   string   `json:"placeholder,omitempty"`
	Required      bool     `json:"required"`
	Description   string   `json:"description"`
	Default       string   `json:"default,omitempty"`
	AllowedValues []string `json:"allowed_values,omitempty"`
}

// DirectiveDef defines the specification, syntax, arguments, and documentation for a DSL directive or pipeline stage.
type DirectiveDef struct {
	Name        string   `json:"name"`
	Scopes      []Scope  `json:"scopes"`
	Aliases     []string `json:"aliases,omitempty"`
	ValueHint   string   `json:"value_hint,omitempty"`
	Args        []ArgDef `json:"args,omitempty"`
	Description string   `json:"description"`
	Example     string   `json:"example"`
}

// HasScope reports whether the directive is valid within the given declaration scope.
func (d *DirectiveDef) HasScope(s Scope) bool {
	for _, sc := range d.Scopes {
		if sc == s {
			return true
		}
	}

	return false
}

// Registry is the canonical list of all directives and pipeline stages supported by aoni-gen.
var Registry = []*DirectiveDef{
	// ==========================================
	// 1. SERVICE SCOPE DIRECTIVES
	// ==========================================
	{
		Name:        "aoni:service",
		Scopes:      []Scope{ScopeService},
		Aliases:     []string{"service"},
		Description: "Marks an interface as a declarative API contract for client code generation.",
		Args: []ArgDef{
			{
				Name:        "name",
				Placeholder: "\"<name>\"",
				Description: "Custom generated client struct name (defaults to unexported camelCase)",
			},
			{
				Name:          "casing",
				Placeholder:   "<style>",
				Description:   "Default wire parameter casing style",
				AllowedValues: []string{"snake_case", "flatcase", "camelcase", "kebab-case", "pascalcase", "none"},
			},
			{Name: "prefix", Placeholder: "\"<path>\"", Description: "Path prefix prepended to all method routes"},
		},
		Example: "// @aoni:service casing=snake_case\ntype GitHubAPI interface { ... }",
	},
	{
		Name:        "base_url",
		Scopes:      []Scope{ScopeService},
		ValueHint:   "\"https://api.example.com/v1\"",
		Description: "Sets the service-wide base URL endpoint.",
		Example:     "// @base_url \"https://api.github.com\"",
	},
	{
		Name:        "engine",
		Scopes:      []Scope{ScopeService},
		ValueHint:   "fast | std | custom | required",
		Description: "Selects underlying execution engine (fast.Client, net/http, or strictly required custom requester).",
		Args: []ArgDef{
			{
				Name:        "type",
				Placeholder: "\"<pkg.Type>\"",
				Description: "Go interface type for custom requester (e.g. type=\"community.Requester\")",
			},
			{
				Name:          "required",
				Placeholder:   "<bool>",
				Description:   "Enforces non-nil requester argument in New constructor",
				AllowedValues: []string{"true", "false"},
			},
		},
		Example: "// @engine custom type=\"community.Requester\" required",
	},
	{
		Name:        "protocol",
		Scopes:      []Scope{ScopeService},
		ValueHint:   "http | rpc | socket | channel | grpc | ws | ssh",
		Description: "Selects underlying communication protocol.",
		Example:     "// @protocol http",
	},
	{
		Name:        "persona",
		Scopes:      []Scope{ScopeService},
		ValueHint:   "\"chrome_133\" | \"firefox_135\" | \"safari_18\"",
		Description: "Configures Chromium / Firefox / Safari browser impersonation profile.",
		Example:     "// @persona \"chrome_133\"",
	},
	{
		Name:        "tls_spec",
		Scopes:      []Scope{ScopeService},
		ValueHint:   "\"chrome_auto\"",
		Description: "Configures TLS ClientHello fingerprint emulation specification.",
		Example:     "// @tls_spec \"chrome_auto\"",
	},
	{
		Name:        "p0f",
		Scopes:      []Scope{ScopeService},
		ValueHint:   "\"windows\" | \"linux\" | \"macos\" | \"ios\" | \"android\"",
		Description: "Spoofs TCP/IP SYN packet fingerprint (p0f) for OS stack evasion.",
		Example:     "// @p0f \"windows\"",
	},
	{
		Name:        "timeout",
		Scopes:      []Scope{ScopeService, ScopeMethod},
		ValueHint:   "\"5s\" | \"500ms\"",
		Description: "Sets execution timeout for the service or individual method.",
		Example:     "// @timeout \"10s\"",
	},
	{
		Name:        "retry",
		Scopes:      []Scope{ScopeService},
		Description: "Configures automated retry policy with exponential backoff and jitter.",
		Args: []ArgDef{
			{Name: "attempts", Placeholder: "<count>", Description: "Maximum number of retry attempts", Default: "3"},
			{Name: "backoff", Placeholder: "\"<duration>\"", Description: "Initial backoff duration", Default: "100ms"},
			{Name: "max_backoff", Placeholder: "\"<duration>\"", Description: "Maximum backoff cap", Default: "2s"},
			{
				Name:        "on_status",
				Placeholder: "\"<codes>\"",
				Description: "Comma-separated HTTP status codes triggering retry (e.g. \"429,502,503\")",
			},
		},
		Example: "// @retry attempts=3 backoff=\"200ms\" on_status=\"429,502,503\"",
	},
	{
		Name:        "circuit",
		Scopes:      []Scope{ScopeService},
		Description: "Configures automated circuit breaking against upstream outages.",
		Args: []ArgDef{
			{
				Name:        "threshold",
				Placeholder: "<failures>",
				Description: "Consecutive failure threshold before opening circuit",
				Default:     "5",
			},
			{
				Name:        "cooldown",
				Placeholder: "\"<duration>\"",
				Description: "Cool-down duration before half-open state",
				Default:     "30s",
			},
		},
		Example: "// @circuit threshold=5 cooldown=\"30s\"",
	},
	{
		Name:        "auth",
		Scopes:      []Scope{ScopeService},
		Description: "Configures automated authentication header or OAuth2 token exchange.",
		Args: []ArgDef{
			{
				Name:          "type",
				Placeholder:   "<mode>",
				Description:   "Authentication mechanism",
				AllowedValues: []string{"bearer", "static", "oauth2", "custom_provider"},
			},
			{Name: "header", Placeholder: "\"<name>\"", Description: "Custom header name", Default: "Authorization"},
			{Name: "prefix", Placeholder: "\"<prefix>\"", Description: "Token prefix", Default: "Bearer "},
			{Name: "provider", Placeholder: "\"<provider>\"", Description: "Custom session provider interface type"},
		},
		Example: "// @auth type=bearer header=\"Authorization\" prefix=\"Bearer \"",
	},
	{
		Name:        "casing",
		Scopes:      []Scope{ScopeService, ScopeMethod},
		ValueHint:   "snake_case | flatcase | camelCase | kebab-case | PascalCase | none",
		Description: "Sets default wire parameter casing style for service or form payload.",
		Example:     "// @casing snake_case",
	},
	{
		Name:        "header",
		Scopes:      []Scope{ScopeService, ScopeParam},
		ValueHint:   "\"Key: Value\"",
		Description: "Adds a static/dynamic default HTTP header to requests or binds parameter to a header.",
		Example:     "// @header \"User-Agent: Aoni-Client/1.0\"",
	},
	{
		Name:        "envelope",
		Scopes:      []Scope{ScopeService},
		ValueHint:   "\"data\" | \"result\"",
		Description: "Sets default JSON envelope field to unwrap across all response models.",
		Example:     "// @envelope \"data\"",
	},
	{
		Name:        "type_map",
		Scopes:      []Scope{ScopeService},
		ValueHint:   "<Type> -> <Strategy>",
		Description: "Configures package-wide serialization strategy for specific Go types.",
		Example:     "// @type_map time.Time -> unix_s",
	},
	{
		Name:        "ssh",
		Scopes:      []Scope{ScopeService, ScopeMethod},
		Description: "Configures SSH connection parameters or command execution.",
		Args: []ArgDef{
			{Name: "host", Placeholder: "\"<host>\"", Description: "Remote host address"},
			{Name: "user", Placeholder: "\"<user>\"", Description: "Remote SSH user"},
			{Name: "key", Placeholder: "\"<path>\"", Description: "Path to private key file"},
			{
				Name:        "pass_env",
				Placeholder: "\"<var>\"",
				Description: "Environment variable with password or passphrase",
			},
			{
				Name:          "agent",
				Placeholder:   "<bool>",
				Description:   "Use SSH agent for authentication",
				AllowedValues: []string{"true", "false"},
			},
		},
		Example: "// @ssh host=\"prod.server.com\" user=\"deploy\" key=\"~/.ssh/id_ed25519\"",
	},

	// ==========================================
	// 2. SOCKET SCOPE DIRECTIVES
	// ==========================================
	{
		Name:        "aoni:socket",
		Scopes:      []Scope{ScopeSocket},
		Aliases:     []string{"socket"},
		Description: "Marks an interface for persistent multi-core socket facade code generation.",
		Example:     "// @aoni:socket\ntype SteamSocket interface { ... }",
	},
	{
		Name:        "endpoint",
		Scopes:      []Scope{ScopeSocket},
		ValueHint:   "<EndpointType>",
		Description: "Defines the target connection endpoint struct type for connector dialing.",
		Example:     "// @endpoint CMServer",
	},
	{
		Name:        "packet",
		Scopes:      []Scope{ScopeSocket},
		ValueHint:   "<PacketType>",
		Description: "Defines the decoded packet structure passed through processor and dispatcher.",
		Example:     "// @packet *protocol.Packet",
	},
	{
		Name:        "opcode",
		Scopes:      []Scope{ScopeSocket},
		ValueHint:   "<OpCodeType>",
		Description: "Defines the opcode enum type used for message dispatching.",
		Example:     "// @opcode enums.EMsg",
	},
	{
		Name:        "job_id",
		Scopes:      []Scope{ScopeSocket},
		ValueHint:   "<JobIDType>",
		Description: "Defines the integer job ID type used for RPC request/response correlation.",
		Example:     "// @job_id uint64",
	},
	{
		Name:        "heartbeat",
		Scopes:      []Scope{ScopeSocket},
		Description: "Configures background ping/heartbeat loop parameters.",
		Args: []ArgDef{
			{Name: "interval", Placeholder: "\"<duration>\"", Description: "Heartbeat timer interval", Required: true},
			{Name: "opcode", Placeholder: "<opcode>", Description: "Heartbeat message opcode"},
			{Name: "msg", Placeholder: "<constructor>", Description: "Heartbeat payload struct constructor"},
		},
		Example: "// @heartbeat interval=\"10s\"",
	},

	// ==========================================
	// 3. METHOD SCOPE DIRECTIVES
	// ==========================================
	{
		Name:        "get",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "\"/path/{var}\"",
		Description: "Defines an HTTP GET endpoint route with optional path template variables.",
		Example:     "// @get \"users/{username}\"",
	},
	{
		Name:        "post",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "\"/path/{var}\"",
		Description: "Defines an HTTP POST endpoint route with optional path template variables.",
		Example:     "// @post \"repos/{owner}/{repo}/issues\"",
	},
	{
		Name:        "put",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "\"/path/{var}\"",
		Description: "Defines an HTTP PUT endpoint route.",
		Example:     "// @put \"items/{id}\"",
	},
	{
		Name:        "delete",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "\"/path/{var}\"",
		Description: "Defines an HTTP DELETE endpoint route.",
		Example:     "// @delete \"items/{id}\"",
	},
	{
		Name:        "patch",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "\"/path/{var}\"",
		Description: "Defines an HTTP PATCH endpoint route.",
		Example:     "// @patch \"items/{id}\"",
	},
	{
		Name:        "head",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "\"/path/{var}\"",
		Description: "Defines an HTTP HEAD endpoint route.",
		Example:     "// @head \"items/{id}\"",
	},
	{
		Name:        "options",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "\"/path/{var}\"",
		Description: "Defines an HTTP OPTIONS endpoint route.",
		Example:     "// @options \"items/{id}\"",
	},
	{
		Name:        "op",
		Scopes:      []Scope{ScopeMethod},
		Aliases:     []string{"operation"},
		ValueHint:   "<operationName>",
		Description: "Defines a generic RPC request-response operation.",
		Example:     "// @op \"GetUserProfile\"",
	},
	{
		Name:        "notify",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "<operationName>",
		Description: "Defines a one-way fire-and-forget asynchronous notification.",
		Example:     "// @notify \"Ping\"",
	},
	{
		Name:        "event",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "<eventName>",
		Description: "Subscribes to an inbound push event with a typed handler callback.",
		Example:     "// @event \"UserJoined\"",
	},
	{
		Name:        "ws",
		Scopes:      []Scope{ScopeMethod},
		Aliases:     []string{"websocket"},
		ValueHint:   "<event_name>",
		Description: "Configures WebSocket / Socket.IO event emission or subscription.",
		Example:     "// @ws \"chat_message\"",
	},
	{
		Name:        "grpc",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "\"/package.Service/Method\"",
		Description: "Configures gRPC procedure call.",
		Example:     "// @grpc \"/user.UserService/GetUser\"",
	},
	{
		Name:        "form",
		Scopes:      []Scope{ScopeMethod},
		Description: "Encodes request body as application/x-www-form-urlencoded on stack buffer (0 B/op).",
		Args: []ArgDef{
			{
				Name:          "casing",
				Placeholder:   "<style>",
				Description:   "Wire casing for form fields",
				AllowedValues: []string{"snake_case", "flatcase", "camelcase", "kebab-case", "pascalcase", "none"},
			},
		},
		Example: "// @form casing=flatcase",
	},
	{
		Name:        "multipart",
		Scopes:      []Scope{ScopeMethod},
		Description: "Encodes request body as multipart/form-data with zero-alloc boundary streaming.",
		Example:     "// @multipart",
	},
	{
		Name:        "json",
		Scopes:      []Scope{ScopeMethod, ScopePipeline},
		Description: "Serializes request body or decodes response payload as JSON.",
		Example:     "// @json",
	},
	{
		Name:        "proto",
		Scopes:      []Scope{ScopeMethod, ScopePipeline},
		Description: "Serializes request body and deserializes response via Protocol Buffers.",
		Example:     "// @proto",
	},
	{
		Name:        "grpc-web",
		Scopes:      []Scope{ScopeMethod},
		Description: "Encodes request as 5-byte framed gRPC-Web protocol with trailer validation.",
		Example:     "// @grpc-web",
	},
	{
		Name:        "raw",
		Scopes:      []Scope{ScopeMethod},
		Description: "Sends raw binary byte slice or io.Reader stream directly.",
		Example:     "// @raw",
	},
	{
		Name:        "stream",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "sse | ndjson | raw",
		Description: "Enables response streaming mode via Server-Sent Events, NDJSON, or raw chunked channel.",
		Example:     "// @stream sse",
	},
	{
		Name:        "pipeline",
		Scopes:      []Scope{ScopeMethod},
		Aliases:     []string{"return", "body"},
		ValueHint:   "<source> | <stage1> | <stage2> ...",
		Description: "Configures a zero-alloc Wire-Transform pipeline chain for scraping, decompressing, and decoding data.",
		Example:     "// @return body | gzip | json\n// @body json | gzip | base64_url",
	},
	{
		Name:        "extract",
		Scopes:      []Scope{ScopeMethod},
		Description: "Extracts response payload via regular expressions, boundary slicing, or DOM attribute tokens.",
		Args: []ArgDef{
			{
				Name:        "between",
				Placeholder: "\"<prefix> ; <suffix>\"",
				Description: "Zero-alloc prefix and suffix boundary extraction",
			},
			{Name: "regex", Placeholder: "\"<pattern>\"", Description: "Compiled regular expression pattern"},
			{Name: "attr", Placeholder: "\"<attribute>\"", Description: "HTML attribute token extraction"},
			{Name: "css", Placeholder: "\"<selector>\"", Description: "CSS selector for DOM extraction"},
		},
		Example: "// @extract between=\"var config = \";\"\"",
	},
	{
		Name:        "referer",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "<path_template> | :origin | :page | :parent | :self",
		Description: "Generates dynamic Referer header directly on a 128-byte stack buffer (0 B/op).",
		Example:     "// @referer \"profiles/{steamID}/inventory?modal=1\"",
	},
	{
		Name:        "preset",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   ":xhr | :cors | :navigate",
		Description: "Injects browser header presets (X-Requested-With, Sec-Fetch-*, Accept).",
		Example:     "// @preset :xhr",
	},
	{
		Name:        "inject",
		Scopes:      []Scope{ScopeMethod},
		Description: "Performs zero-cost interface assertion on requester to inject dynamic session/CSRF tokens.",
		Args: []ArgDef{
			{
				Name:        "field",
				Placeholder: "\"<field>\"",
				Description: "Target form body field name (e.g. field=\"sessionid\")",
			},
			{
				Name:        "query",
				Placeholder: "\"<param>\"",
				Description: "Target URL query parameter name (e.g. query=\"api_key\")",
			},
			{
				Name:        "header",
				Placeholder: "\"<header>\"",
				Description: "Target HTTP header name (e.g. header=\"X-CSRF-Token\")",
			},
			{
				Name:        "from",
				Placeholder: "\"<getter>\"",
				Description: "Getter method on requester (e.g. from=\"SessionID\")",
				Required:    true,
			},
		},
		Example: "// @inject field=\"sessionid\" from=\"SessionID\"",
	},
	{
		Name:        "check",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "<field> <op> <expected> \"<error_message>\"",
		Description: "Emits post-execution assertion check validating response status or payload properties.",
		Example:     "// @check Success == true \"operation rejected by server\"",
	},
	{
		Name:        "unwrap",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "<fieldName>",
		Description: "Unwraps specific field from JSON response envelope before returning.",
		Example:     "// @unwrap data",
	},
	{
		Name:        "idempotent",
		Scopes:      []Scope{ScopeMethod},
		Aliases:     []string{"idempotency_key"},
		Description: "Injects time-ordered UUIDv7 into Idempotency-Key header on stack buffer (0 B/op).",
		Example:     "// @idempotent",
	},
	{
		Name:        "coalesce",
		Scopes:      []Scope{ScopeMethod},
		Description: "Deduplicates concurrent in-flight requests with identical arguments via SingleFlight.",
		Example:     "// @coalesce",
	},
	{
		Name:        "etag",
		Scopes:      []Scope{ScopeMethod},
		Description: "Enables automatic HTTP 304 conditional ETag caching and If-None-Match headers.",
		Example:     "// @etag",
	},
	{
		Name:        "sign",
		Scopes:      []Scope{ScopeMethod},
		Description: "Calculates cryptographic HMAC request signature and attaches auth headers.",
		Args: []ArgDef{
			{Name: "secret", Placeholder: "\"<secret>\"", Description: "Raw HMAC secret key literal"},
			{
				Name:        "key_env",
				Placeholder: "\"<env_var>\"",
				Description: "Environment variable name containing HMAC secret key",
			},
			{
				Name:          "algo",
				Placeholder:   "<algo>",
				Description:   "Hash algorithm",
				Default:       "sha256",
				AllowedValues: []string{"sha256", "sha512", "sha1"},
			},
			{
				Name:        "header",
				Placeholder: "\"<header>\"",
				Description: "Target signature header name",
				Default:     "X-Signature",
			},
		},
		Example: "// @sign key_env=\"API_SECRET\" algo=\"sha256\" header=\"X-Signature\"",
	},
	{
		Name:        "expect_status",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "<status_code>...",
		Description: "Declares expected success HTTP status codes (returns error if mismatch).",
		Example:     "// @expect_status 200 201 204",
	},
	{
		Name:        "cache",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "\"1m\" | \"30s\"",
		Description: "Enables method-level in-memory response caching TTL.",
		Example:     "// @cache \"5m\"",
	},
	{
		Name:        "call",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "<pkg.Func>",
		Description: "Escape hatch: delegates request execution to custom generic dispatcher function.",
		Example:     "// @call service.WebAPI",
	},
	{
		Name:        "decoder",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "<decoderFunc>",
		Description: "Specifies custom response body decoder function.",
		Example:     "// @decoder json.Unmarshal",
	},
	{
		Name:        "encoder",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "<encoderFunc>",
		Description: "Specifies custom request body encoder function.",
		Example:     "// @encoder json.Marshal",
	},
	{
		Name:        "codec",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "<codecFunc>",
		Description: "Specifies custom combined encoder/decoder codec.",
		Example:     "// @codec mycodec.JSON",
	},

	// ==========================================
	// 4. PARAMETER SCOPE DIRECTIVES
	// ==========================================
	{
		Name:        "query",
		Scopes:      []Scope{ScopeParam},
		ValueHint:   "<wire_name>",
		Description: "Binds function parameter to URL query parameter with zero-alloc string formatting.",
		Example:     "// @query \"page_size\"",
	},
	{
		Name:        "field",
		Scopes:      []Scope{ScopeParam},
		ValueHint:   "<wire_name>",
		Description: "Binds function parameter to application/x-www-form-urlencoded or multipart form field.",
		Example:     "// @field \"tradeofferid\"",
	},
	{
		Name:        "part",
		Scopes:      []Scope{ScopeParam},
		ValueHint:   "<part_name>",
		Description: "Binds function parameter to multipart form part.",
		Example:     "// @part \"metadata\"",
	},
	{
		Name:        "file",
		Scopes:      []Scope{ScopeParam},
		Description: "Binds byte slice, string, or io.Reader to multipart file upload part.",
		Args: []ArgDef{
			{Name: "name", Placeholder: "\"<field>\"", Description: "Multipart field name", Default: "file"},
			{
				Name:        "filename",
				Placeholder: "\"<template>\"",
				Description: "File name literal or dynamic template (e.g. \"{filename}\")",
			},
			{
				Name:        "content_type",
				Placeholder: "\"<mime>\"",
				Description: "MIME content type literal or dynamic template",
			},
		},
		Example: "// @file name=\"avatar\" filename=\"{filename}\" content_type=\"{contentType}\"",
	},
	{
		Name:        "cookie",
		Scopes:      []Scope{ScopeParam},
		ValueHint:   "<cookie_name>",
		Description: "Binds function parameter to Cookie header.",
		Example:     "// @cookie \"session\"",
	},
	{
		Name:        "path",
		Scopes:      []Scope{ScopeParam},
		Aliases:     []string{"param"},
		ValueHint:   "<var_name>",
		Description: "Binds function parameter to URL path template variable.",
		Example:     "// @path \"id\"",
	},
	{
		Name:        "format",
		Scopes:      []Scope{ScopeParam},
		ValueHint:   "unix_s | unix_ms | rfc3339 | bool_int | flag | json | comma | pipe | space | bracket",
		Description: "Specifies serialization format strategy for parameter value.",
		Args: []ArgDef{
			{
				Name:        "layout",
				Placeholder: "\"<layout>\"",
				Description: "Custom time layout string (e.g. layout=\"2006-01-02\")",
			},
		},
		Example: "// @format unix_s",
	},
	{
		Name:        "cast",
		Scopes:      []Scope{ScopeParam},
		ValueHint:   "<GoType>",
		Description: "Applies explicit type casting before serialization.",
		Example:     "// @cast uint64",
	},

	// ==========================================
	// 5. DTO STRUCT SCOPE DIRECTIVES
	// ==========================================
	{
		Name:        "aoni:dto",
		Scopes:      []Scope{ScopeStruct},
		Aliases:     []string{"dto"},
		Description: "Generates compiled AppendFormData and AppendQuery zero-allocation serialization methods.",
		Args: []ArgDef{
			{
				Name:          "casing",
				Placeholder:   "<style>",
				Description:   "Wire naming convention",
				Default:       "snake_case",
				AllowedValues: []string{"snake_case", "flatcase", "camelcase", "kebab-case", "pascalcase", "none"},
			},
			{
				Name:          "omitempty",
				Placeholder:   "<bool>",
				Description:   "Omit zero or empty fields during serialization",
				Default:       "true",
				AllowedValues: []string{"true", "false"},
			},
		},
		Example: "// @aoni:dto casing=snake_case omitempty=true\ntype CreateUserRequest struct { ... }",
	},
	{
		Name:        "aoni:tuple",
		Scopes:      []Scope{ScopeStruct},
		Aliases:     []string{"tuple"},
		Description: "Generates zero-alloc custom UnmarshalJSON decoder for positional JSON arrays (e.g. [12.5, 100, \"ok\"]).",
		Example:     "// @aoni:tuple\ntype GraphPoint struct { Price float64; Volume int64 }",
	},
	{
		Name:        "aoni:union",
		Scopes:      []Scope{ScopeStruct},
		Aliases:     []string{"union"},
		Description: "Generates discriminator-based polymorphism and JSON unmarshaling for tagged unions.",
		Example:     "// @aoni:union\ntype EventVariant struct { ... }",
	},

	// ==========================================
	// 6. PIPELINE STAGE SCOPE DIRECTIVES
	// ==========================================
	{
		Name:        "gzip",
		Scopes:      []Scope{ScopePipeline},
		Description: "Gzip compression / decompression stage in pipeline expressions.",
		Example:     "// @return body | gzip | json\n// @body json | gzip",
	},
	{
		Name:        "gunzip",
		Scopes:      []Scope{ScopePipeline},
		Description: "Decompresses gzip-encoded binary stream into raw bytes.",
		Example:     "// @return body | gunzip | json",
	},
	{
		Name:        "zstd",
		Scopes:      []Scope{ScopePipeline},
		Aliases:     []string{"zstd_decompress"},
		Description: "High-throughput Zstandard compression / decompression stage.",
		Example:     "// @return body | zstd | json",
	},
	{
		Name:        "deflate",
		Scopes:      []Scope{ScopePipeline},
		Aliases:     []string{"flate", "inflate"},
		Description: "Raw Deflate compression / decompression stage.",
		Example:     "// @return body | inflate | json",
	},
	{
		Name:        "snappy",
		Scopes:      []Scope{ScopePipeline},
		Description: "Snappy block compression / decompression stage.",
		Example:     "// @return body | snappy | proto",
	},
	{
		Name:        "base64",
		Scopes:      []Scope{ScopePipeline},
		Aliases:     []string{"base64_decode"},
		Description: "Standard Base64 encoding / decoding stage.",
		Example:     "// @return body | base64_decode | json",
	},
	{
		Name:        "base64_url",
		Scopes:      []Scope{ScopePipeline},
		Aliases:     []string{"base64_url_decode"},
		Description: "URL-safe Base64 encoding / decoding stage (without padding).",
		Example:     "// @body json | gzip | base64_url",
	},
	{
		Name:        "hex",
		Scopes:      []Scope{ScopePipeline},
		Aliases:     []string{"hex_decode"},
		Description: "Hexadecimal byte encoding / decoding stage.",
		Example:     "// @return body | hex_decode | proto",
	},
	{
		Name:        "html_unescape",
		Scopes:      []Scope{ScopePipeline},
		Aliases:     []string{"html_escape"},
		Description: "Unescapes or escapes HTML entities (&quot;, &#39;, &amp;, &lt;, &gt;).",
		Example:     "// @return body | attr(css=\"#cfg\", name=\"data\") | html_unescape | json",
	},
	{
		Name:        "url_unescape",
		Scopes:      []Scope{ScopePipeline},
		Aliases:     []string{"url_escape"},
		Description: "Decodes or encodes URL percent-encoded characters.",
		Example:     "// @return body | query_param(\"token\") | url_unescape",
	},
	{
		Name:        "attr",
		Scopes:      []Scope{ScopePipeline},
		ValueHint:   "attr(css=\"<selector>\", name=\"<attribute>\")",
		Description: "Extracts an HTML attribute value from DOM element matching CSS selector.",
		Args: []ArgDef{
			{
				Name:        "css",
				Placeholder: "\"<selector>\"",
				Description: "CSS selector of the target DOM element",
				Required:    true,
			},
			{
				Name:        "name",
				Placeholder: "\"<attribute>\"",
				Description: "Attribute name to extract (e.g. data-user, value, href)",
				Required:    true,
			},
		},
		Example: "// @return body | attr(css=\"#profile_config\", name=\"data-profile\") | html_unescape | json",
	},
	{
		Name:        "between",
		Scopes:      []Scope{ScopePipeline},
		ValueHint:   "between(prefix=\"<prefix>\", suffix=\"<suffix>\")",
		Description: "Zero-allocation string boundary extraction between prefix and suffix markers.",
		Args: []ArgDef{
			{Name: "prefix", Placeholder: "\"<prefix>\"", Description: "Left boundary marker string", Required: true},
			{Name: "suffix", Placeholder: "\"<suffix>\"", Description: "Right boundary marker string", Required: true},
		},
		Example: "// @return body | between(prefix=\"var g_rgAppContextData = \", suffix=\";\") | json",
	},
	{
		Name:        "regex",
		Scopes:      []Scope{ScopePipeline},
		ValueHint:   "regex(\"<pattern>\")",
		Description: "Extracts matched capturing group via precompiled regular expression.",
		Args: []ArgDef{
			{
				Name:        "pattern",
				Placeholder: "\"<regex>\"",
				Description: "Regular expression pattern containing capture group",
				Required:    true,
			},
		},
		Example: "// @return body | regex(`\"sessionid\":\"([^\"]+)\"`) | json",
	},
	{
		Name:        "css",
		Scopes:      []Scope{ScopePipeline},
		ValueHint:   "css(\"<selector>\")",
		Description: "Extracts inner text content of DOM element matching CSS selector.",
		Example:     "// @return body | css(\".error_box\")",
	},
	{
		Name:        "custom",
		Scopes:      []Scope{ScopePipeline},
		Aliases:     []string{"fn"},
		ValueHint:   "custom=\"<pkg.Func>\"",
		Description: "Executes custom transform function with signature func([]byte) ([]byte, error).",
		Example:     "// @return body | custom=\"steam.DecryptPayload\" | json",
	},
}

// lookupMap stores normalized lowercase directive name/alias to definition mapping.
var lookupMap map[string]*DirectiveDef

func init() {
	lookupMap = make(map[string]*DirectiveDef, len(Registry)*2)
	for _, d := range Registry {
		lookupMap[strings.ToLower(d.Name)] = d
		for _, alias := range d.Aliases {
			lookupMap[strings.ToLower(alias)] = d
		}
	}
}

// Lookup finds a directive definition by its name or alias.
func Lookup(name string) *DirectiveDef {
	return lookupMap[strings.ToLower(strings.TrimPrefix(name, "@"))]
}

// IsKnownDirective reports whether the given name or alias is a registered DSL directive.
func IsKnownDirective(name string) bool {
	return Lookup(name) != nil
}

// ByScope returns all directives valid within a specific declaration scope.
func ByScope(scope Scope) []*DirectiveDef {
	var list []*DirectiveDef
	for _, d := range Registry {
		if d.HasScope(scope) {
			list = append(list, d)
		}
	}

	return list
}

// Scopes returns all supported scope names in logical hierarchy order.
func Scopes() []Scope {
	return []Scope{
		ScopeService,
		ScopeSocket,
		ScopeMethod,
		ScopeParam,
		ScopeStruct,
		ScopePipeline,
	}
}

// GenerateMarkdownTable produces formatted markdown documentation for all directives.
func GenerateMarkdownTable() string {
	var sb strings.Builder

	for _, scope := range Scopes() {
		directives := ByScope(scope)
		if len(directives) == 0 {
			continue
		}

		sort.Slice(directives, func(i, j int) bool {
			return directives[i].Name < directives[j].Name
		})

		title := string(scope)
		if len(title) > 0 {
			title = strings.ToUpper(title[:1]) + title[1:]
		}

		fmt.Fprintf(&sb, "### %s Scope Directives\n\n", title)
		sb.WriteString("| Directive | Arguments / Value | Description |\n")
		sb.WriteString("| :--- | :--- | :--- |\n")

		for _, d := range directives {
			name := "@" + d.Name
			if len(d.Aliases) > 0 {
				name += " (or @" + strings.Join(d.Aliases, ", @") + ")"
			}

			var argsStr []string
			if d.ValueHint != "" {
				argsStr = append(argsStr, "`"+d.ValueHint+"`")
			}

			for _, arg := range d.Args {
				item := "`" + arg.Name + "`"
				if arg.Required {
					item += " *(required)*"
				}

				argsStr = append(argsStr, item)
			}

			argsCol := strings.Join(argsStr, ", ")
			if argsCol == "" {
				argsCol = "—"
			}

			fmt.Fprintf(&sb, "| `%s` | %s | %s |\n", name, argsCol, d.Description)
		}

		sb.WriteString("\n")
	}

	return sb.String()
}
