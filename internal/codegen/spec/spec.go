// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package spec provides the definitive, self-documenting registry of all DSL directives,
// arguments, scopes, and validation rules for aoni-gen.
package spec

import (
	"fmt"
	"sort"
	"strings"
)

// Scope categorizes the declaration level where a directive is valid.
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
)

// ArgDef describes an argument or flag accepted by a directive.
type ArgDef struct {
	Name          string   `json:"name"`
	Required      bool     `json:"required"`
	Description   string   `json:"description"`
	Default       string   `json:"default,omitempty"`
	AllowedValues []string `json:"allowed_values,omitempty"`
}

// DirectiveDef defines the specification, syntax, arguments, and documentation for a DSL directive.
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

// Registry is the canonical list of all directives supported by aoni-gen.
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
			{Name: "name", Description: "Custom generated client struct name (defaults to unexported camelCase)"},
			{
				Name:          "casing",
				Description:   "Default wire casing style (snake_case, flatcase, camelCase, kebab-case, PascalCase, none)",
				AllowedValues: []string{"snake_case", "flatcase", "camelcase", "kebab-case", "pascalcase", "none"},
			},
			{Name: "prefix", Description: "Path prefix prepended to all method routes"},
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
			{Name: "type", Description: "Go interface type for custom requester (e.g. type=\"community.Requester\")"},
			{Name: "required", Description: "Enforces non-nil requester argument in New constructor"},
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
			{Name: "attempts", Description: "Maximum number of retry attempts", Default: "3"},
			{Name: "backoff", Description: "Initial backoff duration", Default: "100ms"},
			{Name: "max_backoff", Description: "Maximum backoff cap", Default: "2s"},
			{
				Name:        "on_status",
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
			{Name: "threshold", Description: "Consecutive failure threshold before opening circuit", Default: "5"},
			{Name: "cooldown", Description: "Cool-down duration before half-open state", Default: "30s"},
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
				Description:   "Auth mechanism (bearer, static, oauth2, custom_provider)",
				AllowedValues: []string{"bearer", "static", "oauth2", "custom_provider"},
			},
			{Name: "header", Description: "Custom header name (default: Authorization)"},
			{Name: "prefix", Description: "Token prefix (e.g. \"Bearer \", \"Token \")"},
			{Name: "provider", Description: "Custom session provider interface type"},
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
			{Name: "host", Description: "Remote host address"},
			{Name: "user", Description: "Remote SSH user"},
			{Name: "key", Description: "Path to private key file"},
			{Name: "pass_env", Description: "Environment variable with password or passphrase"},
			{Name: "agent", Description: "Use SSH agent for authentication"},
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
			{Name: "interval", Description: "Heartbeat timer interval (e.g. \"10s\")", Required: true},
			{Name: "opcode", Description: "Heartbeat message opcode"},
			{Name: "msg", Description: "Heartbeat payload struct constructor"},
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
				Description:   "Wire casing for form fields (snake_case, flatcase, camelCase, kebab-case, none)",
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
		Scopes:      []Scope{ScopeMethod},
		Description: "Serializes request body as JSON.",
		Example:     "// @json",
	},
	{
		Name:        "proto",
		Scopes:      []Scope{ScopeMethod},
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
		Name:        "return",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "<pipeline expression>",
		Description: "Configures a Wire-Transform pipeline chain for scraping, decoding, or transforming responses.",
		Example:     "// @return body | attr(css=\"#cfg\", name=\"data\") | html_unescape | json",
	},
	{
		Name:        "body",
		Scopes:      []Scope{ScopeMethod},
		ValueHint:   "<pipeline expression>",
		Description: "Configures a Wire-Transform pipeline chain for outbound request body payload serialization.",
		Example:     "// @body json | gzip | base64",
	},
	{
		Name:        "extract",
		Scopes:      []Scope{ScopeMethod},
		Description: "Extracts response payload via regular expressions, boundary slicing, or DOM attribute tokens.",
		Args: []ArgDef{
			{
				Name:        "between",
				Description: "Zero-alloc prefix and suffix boundary extraction (e.g. between=\"var g_rgAppContextData = ;\")",
			},
			{Name: "regex", Description: "Compiled regular expression pattern"},
			{Name: "attr", Description: "HTML attribute token extraction"},
			{Name: "css", Description: "CSS selector for DOM extraction"},
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
			{Name: "field", Description: "Target form body field name (e.g. field=\"sessionid\")"},
			{Name: "query", Description: "Target URL query parameter name (e.g. query=\"api_key\")"},
			{Name: "header", Description: "Target HTTP header name (e.g. header=\"X-CSRF-Token\")"},
			{Name: "from", Description: "Getter method on requester (e.g. from=\"SessionID\")", Required: true},
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
			{Name: "secret", Description: "Raw HMAC secret key literal"},
			{Name: "key_env", Description: "Environment variable name containing HMAC secret key"},
			{Name: "algo", Description: "Hash algorithm (sha256, sha512, sha1)", Default: "sha256"},
			{Name: "header", Description: "Target signature header name", Default: "X-Signature"},
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
			{Name: "name", Description: "Multipart field name (default: file)", Default: "file"},
			{Name: "filename", Description: "File name literal or dynamic template (e.g. \"{filename}\")"},
			{Name: "content_type", Description: "MIME content type literal or dynamic template"},
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
			{Name: "layout", Description: "Custom time layout string (e.g. layout=\"2006-01-02\")"},
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
				Description:   "Wire casing convention (snake_case, flatcase, camelCase, kebab-case, PascalCase, none)",
				Default:       "snake_case",
				AllowedValues: []string{"snake_case", "flatcase", "camelcase", "kebab-case", "pascalcase", "none"},
			},
			{
				Name:        "omitempty",
				Description: "Omit zero/empty values during serialization (true | false)",
				Default:     "true",
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
