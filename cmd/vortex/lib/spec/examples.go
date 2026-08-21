// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spec

import (
	"fmt"
	"strings"
)

// ExampleTemplate represents a runnable, self-contained contract template.
type ExampleTemplate struct {
	Kind        string   `json:"kind"`
	Aliases     []string `json:"aliases,omitempty"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	SourceCode  string   `json:"source_code"`
}

// Examples contains reference contracts for HTTP, WebSocket, Streaming, Auth, Scraping, and Sockets.
var Examples = []*ExampleTemplate{
	{
		Kind:        "rest",
		Aliases:     []string{"http", "api", "crud"},
		Title:       "Declarative HTTP / REST Service Contract (CRUD & Pagination)",
		Description: "Complete REST API contract with smart casing, DTO structs, query filters, pagination, and @idempotent.",
		SourceCode: `// Copyright (c) 2026 Example Corp. All rights reserved.
package {{pkgName}}

import (
	"context"

	"github.com/lemon4ksan/aoni"
)

// @aoni:dto casing=snake_case omitempty=true
type CreateItemRequest struct {
	Title       string   ` + "`json:\"title\"`" + `
	Description string   ` + "`json:\"description,omitempty\"`" + `
	Price       uint64   ` + "`json:\"price\"`" + `
	Tags        []string ` + "`json:\"tags,omitempty\"`" + `
}

type UpdateItemRequest struct {
	Title       string ` + "`json:\"title,omitempty\"`" + `
	Description string ` + "`json:\"description,omitempty\"`" + `
	Price       uint64 ` + "`json:\"price,omitempty\"`" + `
}

type Item struct {
	ID          uint64   ` + "`json:\"id\"`" + `
	Title       string   ` + "`json:\"title\"`" + `
	Description string   ` + "`json:\"description,omitempty\"`" + `
	Price       uint64   ` + "`json:\"price\"`" + `
	Tags        []string ` + "`json:\"tags,omitempty\"`" + `
	CreatedAt   int64    ` + "`json:\"created_at\"`" + `
}

type ListItemsResponse struct {
	Items      []Item ` + "`json:\"items\"`" + `
	TotalCount int    ` + "`json:\"total_count\"`" + `
}

// @aoni:service casing=snake_case prefix="/v1"
// @base_url "{{baseURL}}"
type {{serviceName}} interface {
	// @get "items"
	ListItems(ctx context.Context, limit, offset int, mods ...aoni.RequestModifier) (*ListItemsResponse, error)

	// @get "items/{id}"
	GetItem(ctx context.Context, id uint64, mods ...aoni.RequestModifier) (*Item, error)

	// @post "items"
	// @idempotent
	CreateItem(ctx context.Context, req CreateItemRequest, mods ...aoni.RequestModifier) (*Item, error)

	// @put "items/{id}"
	UpdateItem(ctx context.Context, id uint64, req UpdateItemRequest, mods ...aoni.RequestModifier) (*Item, error)

	// @delete "items/{id}"
	DeleteItem(ctx context.Context, id uint64, mods ...aoni.RequestModifier) error
}
`,
	},
	{
		Kind:        "stream",
		Aliases:     []string{"sse", "ndjson", "events"},
		Title:       "Real-Time Server-Sent Events (SSE) & NDJSON Streaming Contract",
		Description: "Real-time streaming client for AI generation, live market feeds, and push event listeners.",
		SourceCode: `// Copyright (c) 2026 Example Corp. All rights reserved.
package {{pkgName}}

import (
	"context"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/realtime"
)

type StreamEvent struct {
	ID        string ` + "`json:\"id\"`" + `
	EventType string ` + "`json:\"event_type\"`" + `
	Payload   string ` + "`json:\"payload\"`" + `
	Timestamp int64  ` + "`json:\"timestamp\"`" + `
}

type GenerateRequest struct {
	Prompt      string  ` + "`json:\"prompt\"`" + `
	Model       string  ` + "`json:\"model\"`" + `
	Temperature float64 ` + "`json:\"temperature,omitempty\"`" + `
}

// @aoni:service casing=snake_case prefix="/v1"
// @base_url "{{baseURL}}"
type {{serviceName}} interface {
	// @post "stream/generate"
	// @stream kind=sse
	StreamGenerate(ctx context.Context, req GenerateRequest, mods ...aoni.RequestModifier) (*realtime.SSEStream[StreamEvent], error)

	// @get "events/live"
	// @stream kind=ndjson
	LiveEvents(ctx context.Context, topic string, mods ...aoni.RequestModifier) (*realtime.NDJSONStream[StreamEvent], error)
}
`,
	},
	{
		Kind:        "ws",
		Aliases:     []string{"websocket", "realtime", "socketio"},
		Title:       "WebSocket & Typed Event Messaging Contract",
		Description: "Declarative WebSocket contract for typed event emission and bi-directional message handlers.",
		SourceCode: `// Copyright (c) 2026 Example Corp. All rights reserved.
package {{pkgName}}

import (
	"context"

	"github.com/lemon4ksan/aoni"
)

type ChatMessage struct {
	RoomID  string ` + "`json:\"room_id\"`" + `
	Sender  string ` + "`json:\"sender\"`" + `
	Content string ` + "`json:\"content\"`" + `
	SentAt  int64  ` + "`json:\"sent_at\"`" + `
}

type SendMessageRequest struct {
	RoomID  string ` + "`json:\"room_id\"`" + `
	Content string ` + "`json:\"content\"`" + `
}

// @aoni:service protocol=ws
// @base_url "wss://ws.example.com/v1"
type {{serviceName}} interface {
	// @ws "send_message"
	SendMessage(ctx context.Context, req SendMessageRequest, mods ...aoni.RequestModifier) error

	// @event "on_message"
	OnMessage(ctx context.Context, handler func(msg ChatMessage)) error
}
`,
	},
	{
		Kind:        "auth",
		Aliases:     []string{"oauth2", "hmac", "security"},
		Title:       "OAuth2, Bearer Token Rotation & HMAC Signatures Contract",
		Description: "Secure client contract with automatic bearer token rotation and cryptographic HMAC request signing.",
		SourceCode: `// Copyright (c) 2026 Example Corp. All rights reserved.
package {{pkgName}}

import (
	"context"

	"github.com/lemon4ksan/aoni"
)

type TokenRequest struct {
	GrantType    string ` + "`json:\"grant_type\"`" + `
	ClientID     string ` + "`json:\"client_id\"`" + `
	ClientSecret string ` + "`json:\"client_secret\"`" + `
	RefreshToken string ` + "`json:\"refresh_token,omitempty\"`" + `
}

type TokenResponse struct {
	AccessToken  string ` + "`json:\"access_token\"`" + `
	TokenType    string ` + "`json:\"token_type\"`" + `
	ExpiresIn    int64  ` + "`json:\"expires_in\"`" + `
	RefreshToken string ` + "`json:\"refresh_token,omitempty\"`" + `
}

type UserProfile struct {
	ID    string ` + "`json:\"id\"`" + `
	Email string ` + "`json:\"email\"`" + `
	Name  string ` + "`json:\"name\"`" + `
	Role  string ` + "`json:\"role\"`" + `
}

// @aoni:service casing=snake_case prefix="/oauth2/v1"
// @base_url "{{baseURL}}"
// @sign hmac-sha256 key_var="API_SECRET" header="X-Signature"
type {{serviceName}} interface {
	// @post "token"
	ExchangeToken(ctx context.Context, req TokenRequest, mods ...aoni.RequestModifier) (*TokenResponse, error)

	// @get "userinfo"
	// @bearer :auto
	GetUserInfo(ctx context.Context, mods ...aoni.RequestModifier) (*UserProfile, error)
}
`,
	},
	{
		Kind:        "stealth",
		Aliases:     []string{"scrape", "crawler", "browser", "pipeline", "scraper", "scraping"},
		Title:       "Stealth Scraping, p0f OS Spoofing & DOM Extraction Contract",
		Description: "Anti-bot evasion contract with Chromium impersonation, p0f OS spoofing, JA4 fingerprinting, and CSS slicing.",
		SourceCode: `// Copyright (c) 2026 Example Corp. All rights reserved.
package {{pkgName}}

import (
	"context"

	"github.com/lemon4ksan/aoni"
)

type ScrapedProfile struct {
	Username   string ` + "`json:\"username\"`" + `
	AvatarURL  string ` + "`json:\"avatar_url\"`" + `
	Followers  int    ` + "`json:\"followers\"`" + `
	IsVerified bool   ` + "`json:\"is_verified\"`" + `
}

// @aoni:service
// @base_url "https://target-website.com"
// @chrome version="133"
// @p0f os="windows"
type {{serviceName}} interface {
	// @get "users/{username}"
	// @return body | attr(css=".user-card", name="data-json") | json
	GetUserProfile(ctx context.Context, username string, mods ...aoni.RequestModifier) (*ScrapedProfile, error)

	// @get "listings/{category}"
	// @return body | between(prefix="window.__INITIAL_DATA__ = ", suffix=";</script>") | json
	GetCategoryListings(ctx context.Context, category string, mods ...aoni.RequestModifier) (map[string]any, error)
}
`,
	},
	{
		Kind:        "grpc",
		Aliases:     []string{"grpc-web", "protobuf", "rpc"},
		Title:       "Framed gRPC-Web & Protobuf Microservice Contract",
		Description: "High-performance framed RPC client with 5-byte framing and trailer validation.",
		SourceCode: `// Copyright (c) 2026 Example Corp. All rights reserved.
package {{pkgName}}

import (
	"context"

	"github.com/lemon4ksan/aoni"
)

// @aoni:dto
type PingRequest struct {
	Payload string ` + "`json:\"payload\"`" + `
}

type PingResponse struct {
	Message string ` + "`json:\"message\"`" + `
	Latency int64  ` + "`json:\"latency_ms\"`" + `
}

// @aoni:service protocol=grpc-web prefix="/v1"
// @base_url "{{baseURL}}"
type {{serviceName}} interface {
	// @post "ping"
	// @grpc_web
	Ping(ctx context.Context, req PingRequest, mods ...aoni.RequestModifier) (*PingResponse, error)
}
`,
	},
	{
		Kind:        "socket",
		Aliases:     []string{"tcp"},
		Title:       "High-Throughput Multi-Core Socket Facade Contract",
		Description: "Persistent socket facade with automated heartbeats, typed opcodes, and job ID correlation.",
		SourceCode: `// Copyright (c) 2026 Example Corp. All rights reserved.
package {{pkgName}}

import (
	"context"
	"time"
)

type CMServer struct {
	Host string
	Port uint16
}

type Packet struct {
	OpCode uint32
	JobID  uint64
	Body   []byte
}

type LogonReq struct {
	AccountName string
	Password    string
}

type LogonResp struct {
	Result  int
	SteamID uint64
	Session uint32
}

// @aoni:socket
// @endpoint CMServer
// @packet *Packet
// @opcode uint32
// @job_id uint64
// @heartbeat interval="10s" opcode=1002
type {{serviceName}} interface {
	// @op 1001
	Logon(ctx context.Context, req LogonReq, timeout time.Duration) (*LogonResp, error)

	// @notify 1003
	SendHeartbeat(ctx context.Context) error

	// @event 2001
	OnServerMessage(ctx context.Context, handler func(pkt *Packet)) error
}
`,
	},
}

// exampleMap maps kind/alias to template.
var exampleMap map[string]*ExampleTemplate

func init() {
	exampleMap = make(map[string]*ExampleTemplate, len(Examples)*3)
	for _, ex := range Examples {
		exampleMap[strings.ToLower(ex.Kind)] = ex
		for _, alias := range ex.Aliases {
			exampleMap[strings.ToLower(alias)] = ex
		}
	}
}

// LookupExample retrieves an example template by kind or alias.
func LookupExample(name string) *ExampleTemplate {
	return exampleMap[strings.ToLower(strings.TrimSpace(name))]
}

// RenderTemplate renders a template replacing placeholders with concrete package and service names.
func RenderTemplate(kind, pkgName, serviceName, baseURL string) (string, error) {
	ex := LookupExample(kind)
	if ex == nil {
		return "", fmt.Errorf("unknown template kind %q", kind)
	}

	if pkgName == "" {
		pkgName = "api"
	}

	if serviceName == "" {
		serviceName = toPascalCase(pkgName)
		if !strings.HasSuffix(serviceName, "API") && !strings.HasSuffix(serviceName, "Client") &&
			!strings.HasSuffix(serviceName, "Service") {
			serviceName += "API"
		}
	}

	if baseURL == "" {
		baseURL = "https://api.example.com"
	}

	src := ex.SourceCode
	src = strings.ReplaceAll(src, "{{pkgName}}", pkgName)
	src = strings.ReplaceAll(src, "{{serviceName}}", serviceName)
	src = strings.ReplaceAll(src, "{{baseURL}}", baseURL)

	return src, nil
}

func toPascalCase(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "API"
	}

	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == '/' || r == '\\' || r == '.'
	})

	var sb strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}

		sb.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}

	res := sb.String()
	if res == "" {
		return "API"
	}

	return res
}

// ExampleKinds returns all primary example kinds in display order.
func ExampleKinds() []string {
	kinds := make([]string, 0, len(Examples))
	for _, ex := range Examples {
		kinds = append(kinds, ex.Kind)
	}

	return kinds
}

// PrintExampleHelp outputs an overview of all available templates.
func PrintExampleHelp() string {
	var sb strings.Builder
	sb.WriteString("Vortex Ready-Made Contract Templates & Starters\n")
	sb.WriteString("===============================================\n\n")
	sb.WriteString("Usage:\n")
	sb.WriteString("  vortex init <pkg> -tpl=<kind>\n")
	sb.WriteString("  vortex example <kind> [-out=<file>]\n\n")
	sb.WriteString("Available Starters:\n")

	for _, ex := range Examples {
		aliases := ""
		if len(ex.Aliases) > 0 {
			aliases = fmt.Sprintf(" (alias: %s)", strings.Join(ex.Aliases, ", "))
		}

		fmt.Fprintf(&sb, "  • %-10s%s\n", ex.Kind, aliases)
		fmt.Fprintf(&sb, "    %s\n", ex.Title)
		fmt.Fprintf(&sb, "    %s\n\n", ex.Description)
	}

	sb.WriteString("Examples:\n")
	sb.WriteString("  vortex init billing                     # Scaffold standard REST CRUD package in pkg/billing\n")
	sb.WriteString("  vortex init chat -tpl=ws                # Scaffold WebSocket real-time client\n")
	sb.WriteString("  vortex init ai -tpl=sse                 # Scaffold Server-Sent Events streaming client\n")
	sb.WriteString("  vortex init market -tpl=stealth         # Scaffold anti-bot web scraping client\n")
	sb.WriteString("  vortex init auth -from=swagger.json     # Ingest OpenAPI spec into new package\n")

	return sb.String()
}
