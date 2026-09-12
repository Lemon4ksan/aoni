# Vortex DSL & Contract Inspector

`vortex` is the AST-driven toolchain and static contract inspector that compiles declarative API contracts into zero-allocation network clients.

## 1. Architectural Scope
1. **Method Invariant**: One interface method maps deterministically to one network request or session.
2. **Separation of Concerns**: Contract (DSL) defines remote interface; Infrastructure (`ClientOption`) configures pools/TLS; Context (`RequestModifier`) injects runtime variables.
3. **Zero-Allocation**: Compiles static routes, stack buffers (`[256]byte`), and direct type encoders.

## 2. Formal Grammar (EBNF)

```ebnf
DirectiveComment  ::= "//" WS* "@" DirectiveName [ WS+ DirectiveValue ] [ WS+ DirectiveArgList ]
DirectiveName     ::= [a-zA-Z0-9_:-]+
DirectiveValue    ::= StringLiteral | Identifier
DirectiveArgList  ::= DirectiveArg ( WS+ DirectiveArg )*
DirectiveArg      ::= Identifier "=" ( StringLiteral | Identifier | Number )
StringLiteral     ::= '"' ( [^"\\] | '\\' . )* '"'
Identifier        ::= [a-zA-Z_][a-zA-Z0-9_]*
WS                ::= ' ' | '\t'
```

## 3. Diagnostic Linter (`vortex check`)

| Rule ID | Rule Name | Severity | Fixable? | Description |
| :--- | :--- | :--- | :--- | :--- |
| `E001` | `stale-codegen` | `ERROR` | Yes | Target `*.gen.go` is missing or out-of-sync |
| `E002` | `unmatched-path` | `ERROR` | No | URL path `{variable}` has no matching parameter |
| `E006` | `conflicting-payload` | `ERROR` | No | Mutually exclusive body formats specified |
| `P001` | `missing-dto-encoder` | `WARN` | Yes | Struct payload lacks `@aoni:dto` encoder |
| `P004` | `oversized-stack-frame` | `WARN` | No | Static buffer size > 2KB stack threshold |
| `S001` | `sensitive-query-param` | `WARN` | No | Sensitive credential in URL query string |
| `W003` | `http-verb-mismatch` | `WARN` | No | Read-only prefix annotated with `@post` |
