// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spec

import (
	"fmt"
	"sort"
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

// Examples contains reference contracts for HTTP, WebSocket, Socket, and Wire-Transform pipelines.
var Examples = []*ExampleTemplate{
	{
		Kind:        "http",
		Aliases:     []string{"rest", "api"},
		Title:       "Declarative HTTP / REST Service Contract",
		Description: "Complete REST API contract with smart casing, DTO structs, Referer buffers, and injector assertions.",
		SourceCode: `// Copyright (c) 2026 Example Corp. All rights reserved.
package api

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

type Item struct {
	ID        uint64 ` + "`json:\"id\"`" + `
	Title     string ` + "`json:\"title\"`" + `
	Price     uint64 ` + "`json:\"price\"`" + `
	CreatedAt int64  ` + "`json:\"created_at\"`" + `
}

type ItemResponse struct {
	Success bool  ` + "`json:\"success\"`" + `
	Item    *Item ` + "`json:\"item,omitempty\"`" + `
}

// @aoni:service casing=snake_case prefix="/api/v1"
// @base_url "https://api.example.com"
// @header "User-Agent: MyApp/1.0"
type ItemService interface {
	// @get "items/{id}"
	// @referer :origin
	GetItem(ctx context.Context, id uint64, mods ...aoni.RequestModifier) (*ItemResponse, error)

	// @post "items"
	// @json
	// @idempotent
	// @check Success == true "failed to create item"
	CreateItem(ctx context.Context, req CreateItemRequest, mods ...aoni.RequestModifier) (*ItemResponse, error)

	// @post "items/{id}/buy"
	// @form casing=snake_case
	// @inject field="session_id" from="SessionID"
	BuyItem(ctx context.Context, id uint64, quantity int, mods ...aoni.RequestModifier) (*ItemResponse, error)
}
`,
	},
	{
		Kind:        "ws",
		Aliases:     []string{"websocket", "realtime"},
		Title:       "WebSocket & Real-Time Event Contract",
		Description: "Declarative WebSocket contract for typed event emission and inbound subscription handlers.",
		SourceCode: `// Copyright (c) 2026 Example Corp. All rights reserved.
package realtime

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
type ChatClient interface {
	// @ws "send_message"
	SendMessage(ctx context.Context, req SendMessageRequest, mods ...aoni.RequestModifier) error

	// @event "on_message"
	OnMessage(ctx context.Context, handler func(msg ChatMessage)) error
}
`,
	},
	{
		Kind:        "socket",
		Aliases:     []string{"tcp", "rpc"},
		Title:       "High-Throughput Multi-Core Socket Facade Contract",
		Description: "Persistent socket facade with automated heartbeats, typed opcodes, and job ID correlation.",
		SourceCode: `// Copyright (c) 2026 Example Corp. All rights reserved.
package socket

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
type SteamSocket interface {
	// @op 1001
	Logon(ctx context.Context, req LogonReq, timeout time.Duration) (*LogonResp, error)

	// @notify 1003
	SendHeartbeat(ctx context.Context) error

	// @event 2001
	OnServerMessage(ctx context.Context, handler func(pkt *Packet)) error
}
`,
	},
	{
		Kind:        "pipeline",
		Aliases:     []string{"scraper", "scraping"},
		Title:       "Zero-Alloc Wire-Transform Pipeline & Web Scraping Contract",
		Description: "Scraping and decompression pipeline contract for HTML attributes, boundary slicing, and JSON.",
		SourceCode: `// Copyright (c) 2026 Example Corp. All rights reserved.
package scraper

import (
	"context"

	"github.com/lemon4ksan/aoni"
)

type ProfileConfig struct {
	SteamID    string ` + "`json:\"steamid\"`" + `
	SessionID  string ` + "`json:\"sessionid\"`" + `
	Persona    string ` + "`json:\"persona_name\"`" + `
	IsLoggedIn bool   ` + "`json:\"logged_in\"`" + `
}

// @aoni:service
// @base_url "https://steamcommunity.com"
type ProfileScraper interface {
	// @get "profiles/{steamID}/edit/info"
	// @return body | attr(css="#profile_edit_config", name="data-profile-edit") | html_unescape | json
	GetProfileConfig(ctx context.Context, steamID uint64, mods ...aoni.RequestModifier) (*ProfileConfig, error)

	// @get "market/listings/{appID}/{marketHashName}"
	// @return body | between(prefix="var g_rgAppContextData = ", suffix=";") | json
	GetMarketContext(ctx context.Context, appID uint32, marketHashName string, mods ...aoni.RequestModifier) (map[string]any, error)
}
`,
	},
}

// exampleMap maps kind/alias to template.
var exampleMap map[string]*ExampleTemplate

func init() {
	exampleMap = make(map[string]*ExampleTemplate, len(Examples)*2)
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

// ExampleKinds returns all primary example kinds in display order.
func ExampleKinds() []string {
	kinds := make([]string, 0, len(Examples))
	for _, ex := range Examples {
		kinds = append(kinds, ex.Kind)
	}

	sort.Strings(kinds)

	return kinds
}

// PrintExampleHelp outputs an overview of all available templates.
func PrintExampleHelp() string {
	var sb strings.Builder
	sb.WriteString("aoni-gen Built-In Contract Examples & Templates\n")
	sb.WriteString("===============================================\n\n")
	sb.WriteString("Usage:\n")
	sb.WriteString("  aoni-gen example <kind> [-out=<file>]\n\n")
	sb.WriteString("Available Templates:\n")

	for _, ex := range Examples {
		aliases := ""
		if len(ex.Aliases) > 0 {
			aliases = fmt.Sprintf(" (or %s)", strings.Join(ex.Aliases, ", "))
		}

		fmt.Fprintf(&sb, "  • %-12s%s\n", ex.Kind, aliases)
		fmt.Fprintf(&sb, "    %s\n", ex.Title)
		fmt.Fprintf(&sb, "    %s\n\n", ex.Description)
	}

	sb.WriteString("Examples:\n")
	sb.WriteString("  aoni-gen example http > api.go\n")
	sb.WriteString("  aoni-gen example ws -out=pkg/chat/chat.go\n")
	sb.WriteString("  aoni-gen example socket -out=pkg/socket/socket.go\n")
	sb.WriteString("  aoni-gen example pipeline > scraper.go\n")

	return sb.String()
}
