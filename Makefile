.PHONY: all test race bench fuzz cover lint format clean help

all: format test

test:
	go test -timeout 30s ./...

race:
	go test -race -timeout 90s ./...

bench:
	go test -bench=. -benchmem ./...

fuzz:
	go run ./scripts/fuzz_all.go -fuzztime=2s

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

lint:
	golangci-lint run --timeout=5m ./...

format:
	go fmt ./...
	addlicense -c "Lemon4ksan" -l bsd -ignore "**/*.yml" .
	golangci-lint run --fix ./...

clean:
	go clean -cache -testcache
