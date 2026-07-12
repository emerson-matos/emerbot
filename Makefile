GO ?= go
TOFU ?= tofu

.PHONY: build test fmt lint run-webhook run-cli tofu-fmt tofu-fmt-check tofu-init

build:
	$(GO) build ./...

test:
	$(GO) test ./...

fmt:
	gofumpt -w .

lint:
	golangci-lint run ./...

run-webhook:
	$(GO) run ./apps/webhook/cmd/local

run-cli:
	$(GO) run ./apps/cli/cmd/cli --help

run-lambda:
	$(GO) run ./apps/webhook/cmd/lambda

tofu-fmt:
	$(TOFU) fmt -recursive infra

tofu-fmt-check:
	$(TOFU) fmt -check -recursive infra

tofu-init:
	$(TOFU) -chdir=infra/opentofu/environments/dev init
