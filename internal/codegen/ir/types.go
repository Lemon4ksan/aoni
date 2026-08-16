// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ir

// RootIR represents the parsed root AST containing API services, DTOs, tuples, and unions.
type RootIR struct {
	PackageName            string
	SourceFile             string
	Imports                []ImportIR
	Services               []*ServiceIR
	Structs                []*StructIR
	Tuples                 []*TupleIR
	Unions                 []*UnionIR
	Bitpacks               []*BitpackIR
	UnrecognizedDirectives []UnrecognizedDirectiveIR
}

// UnrecognizedDirectiveIR records unrecognized @directives encountered during parsing for static diagnostic checks.
type UnrecognizedDirectiveIR struct {
	Target string
	Name   string
}

// ImportIR holds a single Go package import dependency.
type ImportIR struct {
	Alias string
	Path  string
}

// ServiceIR represents an API service interface definition and its global client configurations.
type ServiceIR struct {
	Name               string
	Doc                []string
	Protocol           ProtocolKind
	Engine             EngineKind
	CustomEngine       string
	RequesterType      string
	BaseURL            string
	SubRequesters      []SubRequesterIR
	Persona            string
	TLSSpec            string
	P0fOS              string
	Timeout            string
	Retry              *RetryIR
	Circuit            *CircuitBreakerIR
	AuthStrategy       *AuthStrategyIR
	SSHConfig          *SSHConfigIR
	SocketConfig       *SocketConfigIR
	Headers            []HeaderIR
	Envelope           *EnvelopeIR
	TypeMaps           map[string]FormatStrategy
	DefaultCasing      CasingStrategy
	DefaultUnwrapField string
	DefaultErrorModel  string
	Deprecation        *DeprecationIR
	Summary            string
	Description        string
	Tags               []string
	Version            string
	Source             string
	Methods            []*MethodIR
}

// DeprecationIR captures lifecycle deprecation metadata for services, methods, parameters, and fields.
type DeprecationIR struct {
	Reason      string
	Replacement string
	Since       string
	Deadline    string
}

// SocketConfigIR holds configuration parameters for generated generic socket facades.
type SocketConfigIR struct {
	PacketType   string
	OpCodeType   string
	JobIDType    string
	EndpointType string
	Heartbeat    *HeartbeatIR
}

// HeartbeatIR configures background ping/heartbeat loop parameters.
type HeartbeatIR struct {
	Interval string
	OpCode   string
	MsgType  string
}

// ProtocolKind identifies the underlying transport protocol.
type ProtocolKind string

const (
	// ProtocolHTTP represents standard HTTP/1.1, HTTP/2, and HTTP/3 transport.
	ProtocolHTTP ProtocolKind = "http"

	// ProtocolRPC represents a universal RPC / binary socket / broker protocol.
	ProtocolRPC ProtocolKind = "rpc"

	// ProtocolSocket represents a persistent multi-core socket engine.
	ProtocolSocket ProtocolKind = "socket"

	// ProtocolChannel represents channel / event streaming transport.
	ProtocolChannel ProtocolKind = "channel"

	// ProtocolGRPC represents native gRPC framing and streaming.
	ProtocolGRPC ProtocolKind = "grpc"

	// ProtocolWS represents WebSocket and Socket.IO real-time event communication.
	ProtocolWS ProtocolKind = "ws"

	// ProtocolSSH represents SSH command execution and interactive sessions.
	ProtocolSSH ProtocolKind = "ssh"
)

// EngineKind identifies the execution client engine to use.
type EngineKind string

const (
	// EngineFast selects the high-throughput fasthttp + H2/H3 engine.
	EngineFast EngineKind = "fast"

	// EngineNetHTTP selects the standard net/http engine.
	EngineNetHTTP EngineKind = "net/http"

	// EngineCustom selects an externally provided doer or requester implementation.
	EngineCustom EngineKind = "custom"

	// EngineRequired enforces an explicitly provided, authenticated doer/requester at construction time.
	EngineRequired EngineKind = "required"
)

// SubRequesterIR defines an isolated Requester client instance partitioned by distinct BaseURLs.
type SubRequesterIR struct {
	FieldName string
	BaseURL   string
}

// RetryIR defines the retry backoff and jitter policy for requests.
type RetryIR struct {
	Attempts int
	Backoff  string
	Jitter   string
	OnStatus []int
}

// CircuitBreakerIR defines failure threshold and cooldown settings for circuit breaking.
type CircuitBreakerIR struct {
	Threshold int
	Cooldown  string
}

// AuthStrategyIR defines dynamic session authentication and token refresh strategies.
type AuthStrategyIR struct {
	Kind         AuthKind
	HeaderName   string
	ValuePrefix  string
	ProviderType string
}

// AuthKind categorizes the authentication mechanism.
type AuthKind string

const (
	// AuthStatic applies fixed authorization headers.
	AuthStatic AuthKind = "static"

	// AuthBearer applies standard Bearer tokens.
	AuthBearer AuthKind = "bearer"

	// AuthOAuth2 manages automated OAuth2 token exchanges.
	AuthOAuth2 AuthKind = "oauth2"

	// AuthCustomProvider invokes a user-defined session handshake provider.
	AuthCustomProvider AuthKind = "custom_provider"
)

// SSHConfigIR stores SSH connection parameters.
type SSHConfigIR struct {
	Host       string
	User       string
	KeyPath    string
	AgentAuth  bool
	PassEnvVar string
}

// MethodIR represents an endpoint operation or RPC call.
type MethodIR struct {
	Name            string
	Doc             []string
	Operation       OperationKind
	StreamDirection StreamDirection
	HTTPMethod      string
	TargetRequester string
	Path            *PathIR
	PayloadKind     PayloadKind
	StreamKind      StreamKind
	EventName       string
	SSHCommand      string
	Headers         []HeaderIR
	Params          []*ParamIR
	Return          *ReturnIR
	Checks          []CheckIR
	LocalTimeout    string
	LocalCacheTTL   string
	ExpectStatus    []int
	UnwrapField     string
	Decoder         string
	Encoder         string
	Codec           string
	Extract         *ExtractIR
	ReturnPipeline  *PipelineIR
	BodyPipeline    *PipelineIR
	CallFunc        string
	Idempotent      bool
	Coalesce        bool
	ETag            bool
	SignHMAC        *SignHMACIR
	Injects         []InjectIR
	Presets         []string
	OpID            string
	OpIDIsQuoted    bool
	OperationID     string
	Deprecation     *DeprecationIR
	Summary         string
	Description     string
	Tags            []string
	Version         string
	Since           string
	IsNotify        bool
	IsEvent         bool
	FormCasing      CasingStrategy
	QueryCasing     CasingStrategy
	StackModsSize   int
	StackBufSize    int
}

// InjectTarget specifies where an injected variable is placed in the network transaction.
type InjectTarget string

const (
	// InjectField injects a parameter into application/x-www-form-urlencoded body.
	InjectField InjectTarget = "field"

	// InjectQuery injects a parameter into the URL query string.
	InjectQuery InjectTarget = "query"

	// InjectHeader injects a header value.
	InjectHeader InjectTarget = "header"
)

// InjectIR defines dynamic injection of session tokens, CSRF keys, or credentials from the requester.
type InjectIR struct {
	Target     InjectTarget
	WireKey    string
	ProviderFn string
}

// SignHMACIR models cryptographic HMAC request signing settings.
type SignHMACIR struct {
	SecretKey  string
	KeyEnv     string
	Algorithm  string
	HeaderName string
}

// OperationKind specifies the type of method operation.
type OperationKind string

const (
	// OpHTTP represents standard HTTP request-response.
	OpHTTP OperationKind = "http"

	// OpRPC represents a universal request-response RPC.
	OpRPC OperationKind = "rpc"

	// OpNotify represents a one-way fire-and-forget message.
	OpNotify OperationKind = "notify"

	// OpEvent represents an inbound event subscription with a typed handler.
	OpEvent OperationKind = "event"

	// OpGRPC represents a gRPC RPC invocation.
	OpGRPC OperationKind = "grpc"

	// OpWSEmit represents sending a fire-and-forget WebSocket event.
	OpWSEmit OperationKind = "ws_emit"

	// OpWSEmitWithAck represents sending a WebSocket event expecting an acknowledgement payload.
	OpWSEmitWithAck OperationKind = "ws_emit_with_ack" //nolint:gosec // Not credentials, WebSocket ack operation name

	// OpWSOn represents subscribing to incoming WebSocket events with a handler.
	OpWSOn OperationKind = "ws_on"

	// OpSSHExec represents executing a remote SSH command.
	OpSSHExec OperationKind = "ssh_exec"

	// OpSSHShell represents starting an interactive remote SSH shell session.
	OpSSHShell OperationKind = "ssh_shell"

	// OpClose represents closing and unregistering all active subscriptions.
	OpClose OperationKind = "close"
)

// StreamDirection specifies data streaming direction for gRPC or WebSocket.
type StreamDirection string

const (
	// StreamNone indicates a unary non-streaming call.
	StreamNone StreamDirection = "none"

	// StreamServer indicates the server streams messages to the client.
	StreamServer StreamDirection = "server"

	// StreamClient indicates the client streams messages to the server.
	StreamClient StreamDirection = "client"

	// StreamBidi indicates bidirectional streaming.
	StreamBidi StreamDirection = "bidi"
)

// PayloadKind specifies how request bodies are serialized.
type PayloadKind string

const (
	// PayloadJSON encodes the request body as JSON.
	PayloadJSON PayloadKind = "json"

	// PayloadForm encodes the request body as application/x-www-form-urlencoded.
	PayloadForm PayloadKind = "form"

	// PayloadMultipart encodes the request body as multipart/form-data.
	PayloadMultipart PayloadKind = "multipart"

	// PayloadProto encodes the request body as Protocol Buffers.
	PayloadProto PayloadKind = "proto"

	// PayloadGRPCWeb encodes the request body as 5-byte framed gRPC-Web proto.
	PayloadGRPCWeb PayloadKind = "grpc-web"

	// PayloadRaw sends raw byte buffers or strings directly.
	PayloadRaw PayloadKind = "raw"

	// PayloadNone indicates no request body payload.
	PayloadNone PayloadKind = "none"
)

// ExtractKind categorizes HTML/DOM/Script scraping extractors.
type ExtractKind string

const (
	// ExtractRegex extracts payload via compiled regular expressions.
	ExtractRegex ExtractKind = "regex"

	// ExtractBetween extracts payload between prefix and suffix boundaries with zero allocations.
	ExtractBetween ExtractKind = "between"

	// ExtractHTMLToken extracts payload from specific HTML element attributes using html.Tokenizer.
	ExtractHTMLToken ExtractKind = "html_token"

	// ExtractCustom delegates scraping to a user-provided extraction function.
	ExtractCustom ExtractKind = "custom"
)

// ExtractIR defines response payload extraction and HTML/DOM scraping rules.
type ExtractIR struct {
	Kind         ExtractKind
	RegexPattern string
	Prefix       string
	Suffix       string
	Tag          string
	ID           string
	Attr         string
	CustomFunc   string
}

// StreamKind identifies response streaming encoding.
type StreamKind string

const (
	// StreamKindNone indicates standard non-streaming body.
	StreamKindNone StreamKind = "none"

	// StreamKindSSE indicates Server-Sent Events (text/event-stream).
	StreamKindSSE StreamKind = "sse"

	// StreamKindNDJSON indicates Newline-Delimited JSON streaming.
	StreamKindNDJSON StreamKind = "ndjson"

	// StreamKindRawBytes indicates raw chunked byte streaming via channels or io.Reader.
	StreamKindRawBytes StreamKind = "raw_bytes"
)

// PipelineStageType categorizes serialization, compression, encoding, scraping, or custom transforms.
type PipelineStageType string

const (
	// Serialization stages
	StageJSON    PipelineStageType = "json"
	StageProto   PipelineStageType = "proto"
	StageForm    PipelineStageType = "form"
	StageXML     PipelineStageType = "xml"
	StageCBOR    PipelineStageType = "cbor"
	StageMsgPack PipelineStageType = "msgpack"
	StageTuple   PipelineStageType = "tuple"

	// Compression stages
	StageGzip           PipelineStageType = "gzip"
	StageGunzip         PipelineStageType = "gunzip"
	StageZstd           PipelineStageType = "zstd"
	StageZstdDecompress PipelineStageType = "zstd_decompress"
	StageDeflate        PipelineStageType = "deflate"
	StageInflate        PipelineStageType = "inflate"
	StageSnappy         PipelineStageType = "snappy"

	// Encoding stages
	StageBase64          PipelineStageType = "base64"
	StageBase64Decode    PipelineStageType = "base64_decode"
	StageBase64URL       PipelineStageType = "base64_url"
	StageBase64URLDecode PipelineStageType = "base64_url_decode"
	StageHex             PipelineStageType = "hex"
	StageHexDecode       PipelineStageType = "hex_decode"
	StageURLEscape       PipelineStageType = "url_escape"
	StageURLUnescape     PipelineStageType = "url_unescape"
	StageHTMLEscape      PipelineStageType = "html_escape"
	StageHTMLUnescape    PipelineStageType = "html_unescape"

	// Extraction / Scraping stages
	StageRegex   PipelineStageType = "regex"
	StageBetween PipelineStageType = "between"
	StageAttr    PipelineStageType = "attr"

	// Cryptographic / Security stages
	StageHMACSHA256 PipelineStageType = "hmac_sha256"

	// Custom functions
	StageCustom PipelineStageType = "custom"
)

// PipelineStageIR represents a single step in a Wire-Transform pipeline.
type PipelineStageIR struct {
	Type      PipelineStageType
	RawName   string
	FuncExpr  string
	Args      []string
	NamedArgs map[string]string
}

// PipelineIR defines a composite data transformation chain.
type PipelineIR struct {
	Source    string // "body", "header", etc.
	SourceArg string // Header name if Source == "header"
	Stages    []PipelineStageIR
}

// ParamIR models an individual function parameter and its HTTP/RPC binding.
type ParamIR struct {
	GoName      string
	GoType      GoTypeIR
	Location    ParamLocation
	WireKey     string
	Formatter   FormatStrategy
	TimeLayout  string
	FileName    string
	ContentType string
	Pipeline    *PipelineIR
	Deprecation *DeprecationIR
	Description string
	Since       string
}

// ParamLocation specifies where a parameter is mapped in the network transaction.
type ParamLocation string

const (
	// LocContext maps to context.Context.
	LocContext ParamLocation = "context"

	// LocPath maps to a {var} segment in the URL path template.
	LocPath ParamLocation = "path"

	// LocQuery maps to a single ?key=val query parameter.
	LocQuery ParamLocation = "query"

	// LocQueryStruct maps a whole struct to query parameters via compile-time encoding.
	LocQueryStruct ParamLocation = "query_struct"

	// LocHeader maps to a static or formatted request header.
	LocHeader ParamLocation = "header"

	// LocHeaderDynamic maps a function argument directly into a templated header (like Referer).
	LocHeaderDynamic ParamLocation = "header_dyn"

	// LocBody maps to the HTTP/RPC request body payload.
	LocBody ParamLocation = "body"

	// LocFormFields maps struct fields or arguments directly into form body urlencoded bytes.
	LocFormFields ParamLocation = "form_fields"

	// LocMultipartField maps a parameter into a multipart form data field.
	LocMultipartField ParamLocation = "multipart_field"

	// LocMultipartFile maps a parameter into a multipart file upload part.
	LocMultipartFile ParamLocation = "multipart_file"

	// LocModifiers maps variadic ...aoni.RequestModifier parameters.
	LocModifiers ParamLocation = "modifiers"

	// LocCookie maps to a request cookie.
	LocCookie ParamLocation = "cookie"

	// LocBufferReceiver maps a user-provided []byte buffer for zero-alloc body reading.
	LocBufferReceiver ParamLocation = "buf_receiver"

	// LocArenaScope maps memory allocation to an off-heap Arena.
	LocArenaScope ParamLocation = "arena_scope"

	// LocHandler maps an event subscription callback handler.
	LocHandler ParamLocation = "handler"

	// LocStreamInput maps an incoming <-chan T for client-streaming.
	LocStreamInput ParamLocation = "stream_in"

	// LocEventHandler maps an event receiver callback function for WebSocket On events.
	LocEventHandler ParamLocation = "event_handler"
)

// FormatStrategy defines how a parameter is serialized into strings/bytes at compile-time.
type FormatStrategy string

const (
	// FormatDefault applies standard default formatting.
	FormatDefault FormatStrategy = ""

	// FormatDirectString appends string bytes directly.
	FormatDirectString FormatStrategy = "direct_string"

	// FormatQueryEscaped percent-encodes string bytes via urlutil.AppendQueryEscapeString.
	FormatQueryEscaped FormatStrategy = "query_escaped"

	// FormatPathEscaped percent-encodes string bytes for URL path segments.
	FormatPathEscaped FormatStrategy = "path_escaped"

	// FormatIntAppend formats integers via strconv.AppendInt.
	FormatIntAppend FormatStrategy = "strconv_int"

	// FormatUintAppend formats unsigned integers via strconv.AppendUint.
	FormatUintAppend FormatStrategy = "strconv_uint"

	// FormatBoolAppend formats booleans via strconv.AppendBool (true/false).
	FormatBoolAppend FormatStrategy = "strconv_bool"

	// FormatBoolInt formats booleans as 1 or 0 (e.g. key=1).
	FormatBoolInt FormatStrategy = "bool_int"

	// FormatBoolFlag formats booleans as standalone flags without value (e.g. &flag).
	FormatBoolFlag FormatStrategy = "bool_flag"

	// FormatTimeRFC3339 formats time.Time via r.Format(time.RFC3339).
	FormatTimeRFC3339 FormatStrategy = "time_rfc3339"

	// FormatTimeUnixS formats time.Time as Unix epoch seconds (strconv.AppendInt(r.Unix())).
	FormatTimeUnixS FormatStrategy = "time_unix_s"

	// FormatTimeUnixMS formats time.Time as Unix epoch milliseconds (strconv.AppendInt(r.UnixMilli())).
	FormatTimeUnixMS FormatStrategy = "time_unix_ms"

	// FormatTimeLayout formats time.Time using a custom layout (e.g. r.Format("2006-01-02")).
	FormatTimeLayout FormatStrategy = "time_layout"

	// FormatSliceComma formats slices as comma-separated values (e.g. key=a,b,c).
	FormatSliceComma FormatStrategy = "slice_comma"

	// FormatSliceSpace formats slices as space-separated values (e.g. key=a+b+c).
	FormatSliceSpace FormatStrategy = "slice_space"

	// FormatSlicePipe formats slices as pipe-separated values (e.g. key=a|b|c).
	FormatSlicePipe FormatStrategy = "slice_pipe"

	// FormatSliceBracket formats slices as array brackets (e.g. key[]=a&key[]=b).
	FormatSliceBracket FormatStrategy = "slice_bracket"

	// FormatJSONString marshals the struct/value to a JSON string within form fields.
	FormatJSONString FormatStrategy = "json_string"

	// FormatBufferAppender invokes AppendBytes(dst []byte) []byte directly on stack (0 B/op).
	FormatBufferAppender FormatStrategy = "buffer_appender"

	// FormatTextMarshaler invokes MarshalText() on encoding.TextMarshaler types.
	FormatTextMarshaler FormatStrategy = "text_marshaler"

	// FormatCustomStringer formats custom types by casting to underlying primitive or calling String().
	FormatCustomStringer FormatStrategy = "custom_stringer"

	// FormatCompiledEncode invokes a compile-time generated EncodeValues method.
	FormatCompiledEncode FormatStrategy = "compiled_encode"
)

// GoTypeIR represents a Go type identifier with reflection-free type metadata.
type GoTypeIR struct {
	Name         string
	Package      string
	IsPointer    bool
	IsSlice      bool
	IsMap        bool
	IsChannel    bool
	IsVariadic   bool
	IsCustomType bool
	Underlying   string
	ElemType     string
	KeyType      string
}

// ReturnIR models the return signature and response processing pipeline of a method.
type ReturnIR struct {
	SuccessType    GoTypeIR
	StatusMap      map[int]GoTypeIR
	UnionType      *UnionIR
	HasRawResponse bool
	ErrorModelType string
	IsVoid         bool
	IsDirectBytes  bool
	IsStreamChan   bool
}

// UnionIR represents a discriminated union type generated for multi-status responses.
type UnionIR struct {
	Name     string
	Doc      []string
	Fields   []*UnionFieldIR
	Variants map[int]GoTypeIR
}

// UnionFieldIR represents an individual variant field on a multi-status union struct.
type UnionFieldIR struct {
	GoName      string
	Type        GoTypeIR
	StatusCodes []int
}

// EnvelopeIR defines the standard response wrapper schema for unmarshaling and validation.
type EnvelopeIR struct {
	SuccessField string
	DataField    string
	ErrorField   string
}

// CheckIR defines a post-response business condition check on HTTP 200 responses.
type CheckIR struct {
	Field       string
	Operator    CheckOperator
	ExpectedVal string
	ErrorMsg    string
}

// CheckOperator defines comparison operators for CheckIR.
type CheckOperator string

const (
	// OpEqual tests for equality (==).
	OpEqual CheckOperator = "=="

	// OpNotEqual tests for inequality (!=).
	OpNotEqual CheckOperator = "!="
)

// PathIR represents a parsed URI path or dynamic header template with literal and variable segments.
type PathIR struct {
	RawTemplate string
	Segments    []PathSegmentIR
}

// PathSegmentIR represents an individual literal chunk or variable token in a path template.
type PathSegmentIR struct {
	IsVariable bool
	Literal    string
	VarName    string
	Transform  VarTransform
}

// VarTransform defines variable transformation strategies when rendering URL path or header templates.
type VarTransform string

const (
	// TransformNone inserts variable bytes directly.
	TransformNone VarTransform = "none"

	// TransformPathEscape percent-encodes variable bytes for path segments.
	TransformPathEscape VarTransform = "path_escape"

	// TransformQueryEscape percent-encodes variable bytes for query components.
	TransformQueryEscape VarTransform = "query_escape"

	// TransformLower converts string to lowercase.
	TransformLower VarTransform = "lower"

	// TransformUpper converts string to uppercase.
	TransformUpper VarTransform = "upper"
)

// HeaderIR represents a static or dynamically templated HTTP header.
type HeaderIR struct {
	Key             string
	StaticValue     string
	DynamicTemplate *PathIR
}

// StructIR represents a DTO struct definition with casing and serialization rules.
type StructIR struct {
	Name            string
	Doc             []string
	Casing          CasingStrategy
	OmitEmpty       bool
	Fields          []*FieldIR
	GenValueEncoder bool
	Deprecation     *DeprecationIR
	Description     string
	Version         string
	Since           string
}

// FieldIR describes an individual struct field and its wire mapping.
type FieldIR struct {
	GoName      string
	WireName    string
	Type        GoTypeIR
	IsOmitEmpty bool
	CustomTag   string
	Formatter   FormatStrategy
	TimeLayout  string
	Deprecation *DeprecationIR
	Description string
	Since       string
}

// TupleIR defines a heterogeneous JSON tuple array mapping to struct fields by index.
type TupleIR struct {
	Name   string
	Fields []TupleFieldIR
}

// TupleFieldIR maps a specific array index to a struct field.
type TupleFieldIR struct {
	Index  int
	GoName string
	Type   GoTypeIR
}

// CasingStrategy defines field naming transformation conventions.
type CasingStrategy string

const (
	// CasingSnakeCase converts Go PascalCase to snake_case.
	CasingSnakeCase CasingStrategy = "snake_case"

	// CasingCamelCase converts Go PascalCase to camelCase.
	CasingCamelCase CasingStrategy = "camelCase"

	// CasingPascalCase retains PascalCase naming.
	CasingPascalCase CasingStrategy = "PascalCase"

	// CasingKebabCase converts Go PascalCase to kebab-case.
	CasingKebabCase CasingStrategy = "kebab-case"

	// CasingFlatCase converts Go PascalCase to flatcase (all lowercase without separators).
	CasingFlatCase CasingStrategy = "flatcase"

	// CasingNone retains original parameter name without alteration.
	CasingNone CasingStrategy = "none"
)

// Endianness defines byte order for binary encoding.
type Endianness string

const (
	// EndianLittle encodes integers in little-endian byte order.
	EndianLittle Endianness = "little"

	// EndianBig encodes integers in big-endian (network) byte order.
	EndianBig Endianness = "big"
)

// BitpackIR represents a compact bit-packed struct definition.
type BitpackIR struct {
	Name       string
	Doc        []string
	Endianness Endianness
	Fields     []*BitpackFieldIR
	TotalBits  int
	TotalBytes int
}

// BitpackFieldIR represents a single bit-packed field with custom bit-width.
type BitpackFieldIR struct {
	GoName    string
	Type      GoTypeIR
	BitWidth  int
	BitOffset int // Offset in bits from start of packed struct
	Mask      uint64
	IsSigned  bool
	IsBool    bool
}
