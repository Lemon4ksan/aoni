# Discover library packages, excluding examples, scripts, cmd, and vendor
PKG       := $(shell go list ./... | grep -v /examples | grep -v /scripts | grep -v /cmd/ | grep -v /vendor/)
COVER_PKG := $(shell go list ./... | grep -v /examples | grep -v /scripts | grep -v /vendor/)
COVER_OUT ?= coverage.out

# Colors for console output
CYAN  := \033[0;36m
RESET := \033[0m

.PHONY: test race cover cover-clean cover-html lint format clean check-tls-spec update-browsers update-browsers-apply help

test: ## Run quick unit tests
	@printf "$(CYAN)Running unit tests...$(RESET)\n"
	go test -v -timeout 30s $(PKG)

race: ## Run unit tests with race detector enabled
	@printf "$(CYAN)Running tests with race detector...$(RESET)\n"
	go test -v -race -timeout 60s $(PKG)

cover: ## Calculate and print exact core library coverage report
	@printf "$(CYAN)Generating exact coverage report...$(RESET)\n"
	go test -coverpkg=$(COVER_PKG) -coverprofile=$(COVER_OUT) ./...
	go run ./cmd/coverage -file=$(COVER_OUT)

cover-clean: ## Generate clean coverage report and run deduplicated coverage analysis tool
	@printf "$(CYAN)Generating clean coverage report...$(RESET)\n"
	go test -coverpkg=$(COVER_PKG) -coverprofile=$(COVER_OUT) ./...
	go run ./cmd/coverage -file=$(COVER_OUT)

cover-html: cover ## Generate coverage report and open interactive HTML in browser
	@printf "$(CYAN)Opening coverage report in browser...$(RESET)\n"
	go tool cover -html=$(COVER_OUT)

lint: ## Run golangci-lint check
	@printf "$(CYAN)Running linter...$(RESET)\n"
	golangci-lint run ./...

format: ## Format code and auto-fix linter suggestions
	@printf "$(CYAN)Formatting Go code...$(RESET)\n"
	go fmt ./...
	addlicense -c "Lemon4ksan" -l bsd -ignore "**/*.yml" .
	golangci-lint run --fix

clean: ## Delete temporary files, binaries, and coverage profiles
	@printf "$(CYAN)Cleaning up temporary artifacts...$(RESET)\n"
	rm -rf bin/
	rm -f $(COVER_OUT)

check-tls-spec: ## Compare project TLS specs against utls.HelloChrome_Auto / HelloFirefox_Auto
	@printf "$(CYAN)Comparing TLS ClientHello specs...$(RESET)\n"
	go run ./scripts/compare-tls-spec/

update-browsers: ## Dry-run the browser version update script (no files changed)
	@printf "$(CYAN)Updating browser versions (dry-run)...$(RESET)\n"
	bash scripts/update-browser-versions.sh --dry-run

update-browsers-apply: ## Apply browser version updates (Chrome, Firefox, Safari, iOS, Android, utls)
	@printf "$(CYAN)Updating browser versions...$(RESET)\n"
	bash scripts/update-browser-versions.sh

help: ## Show this help message
	@printf "Usage: make [target]\n\nTargets:\n"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

generate:
	cd ".\\cmd\\openapi\\" && go build -o openapi.exe

	".\\cmd\\openapi\\openapi.exe" -spec swagger.json \
        -skip-deprecated -fast \
        -include-path "(v2/classifieds|agent|inventory|classifieds/alerts|notifications)" \
        -include-path "IGet(Prices|Currencies|PriceHistory|Users)" \
        -include-path "users/info"
