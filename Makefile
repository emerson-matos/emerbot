GO      ?= go
TOFU    ?= tofu
NPM     ?= npm
COMPOSE ?= podman compose

# On NixOS / is nearly full; redirect buildah temp files to /home.
export TMPDIR := $(HOME)/.tmp/buildah
$(shell mkdir -p $(TMPDIR))

TOFU_DIR := infra/opentofu/environments/dev
LAMBDA_DIR := $(TOFU_DIR)/.lambdas
LAMBDA_ZIP := $(LAMBDA_DIR)/webhook.zip
DASHBOARD_ZIP := $(LAMBDA_DIR)/dashboard-api.zip

.PHONY: build test fmt lint \
        run-webhook run-api run-cli run-lambda \
        up down up-infra \
        logs-webhook logs-api \
        seed demo \
        web-dev \
        build-lambda-webhook build-lambda-dashboard-api build-lambdas \
        tofu-fmt tofu-fmt-check tofu-init tofu-plan tofu-apply

# ---------------------------------------------------------------------------
# Go
# ---------------------------------------------------------------------------
build:
	$(GO) build ./...

# ---------------------------------------------------------------------------
# Lambda zips (Go -> Linux/arm64 -> bootstrap.zip)
# ---------------------------------------------------------------------------
$(LAMBDA_ZIP):
	mkdir -p $(LAMBDA_DIR)
	GOOS=linux GOARCH=arm64 $(GO) build -o $(LAMBDA_DIR)/webhook-bootstrap ./apps/webhook/cmd/lambda
	cd $(LAMBDA_DIR) && zip -j webhook.zip webhook-bootstrap && rm webhook-bootstrap

$(DASHBOARD_ZIP):
	mkdir -p $(LAMBDA_DIR)
	GOOS=linux GOARCH=arm64 $(GO) build -o $(LAMBDA_DIR)/dashboard-bootstrap ./apps/dashboard-api/cmd/lambda
	cd $(LAMBDA_DIR) && zip -j dashboard-api.zip dashboard-bootstrap && rm dashboard-bootstrap

build-lambda-webhook: $(LAMBDA_ZIP)
build-lambda-dashboard-api: $(DASHBOARD_ZIP)
build-lambdas: build-lambda-webhook build-lambda-dashboard-api

# ---------------------------------------------------------------------------
# Test / fmt / lint
# ---------------------------------------------------------------------------
test:
	$(GO) test ./...

fmt:
	gofumpt -w .

lint:
	golangci-lint run ./...

# ---------------------------------------------------------------------------
# Native Go run (dev, no Docker)
# ---------------------------------------------------------------------------
run-webhook:
	$(GO) run ./apps/webhook/cmd/local

run-api:
	$(GO) run ./apps/dashboard-api/cmd/local

run-cli:
	$(GO) run ./apps/cli/cmd/cli --help

run-lambda:
	$(GO) run ./apps/webhook/cmd/lambda

# ---------------------------------------------------------------------------
# Docker Compose — local stack
# ---------------------------------------------------------------------------
up:
	$(COMPOSE) up --build

down:
	$(COMPOSE) down

up-infra:
	$(COMPOSE) up --build dynamodb-local dynamodb-admin dynamodb-init

logs-webhook:
	$(COMPOSE) logs -f webhook

logs-api:
	$(COMPOSE) logs -f dashboard-api

# ---------------------------------------------------------------------------
# Demo seed
# ---------------------------------------------------------------------------
seed:
	AWS_ACCESS_KEY_ID=local AWS_SECRET_ACCESS_KEY=local AWS_REGION=us-east-1 \
	$(GO) run ./scripts/seed \
		--endpoint http://localhost:8000 \
		--table emerbot-local-financial-entries \
		--user-id demo \
		--months 3

demo: up
	@echo "Waiting for dashboard-api to be healthy..."
	@until wget -qO-  http://localhost:8081/health > /dev/null 2>&1; do sleep 2; done
	$(MAKE) seed
	@echo ""
	@echo "Demo ready!"
	@echo "   Dashboard:       http://localhost:5173"
	@echo "   WhatsApp sim:    http://localhost:9000"
	@echo "   DynamoDB admin:  http://localhost:8001"
	@echo "   Login:           pai@farmacia.local / senha123"

# ---------------------------------------------------------------------------
# Frontend
# ---------------------------------------------------------------------------
web-dev:
	$(NPM) --prefix apps/web run dev

# ---------------------------------------------------------------------------
# OpenTofu
# ---------------------------------------------------------------------------
tofu-fmt:
	$(TOFU) fmt -recursive infra

tofu-fmt-check:
	$(TOFU) fmt -check -recursive infra

tofu-init: build-lambdas
	$(TOFU) -chdir=$(TOFU_DIR) init

tofu-plan: build-lambdas
	$(TOFU) -chdir=$(TOFU_DIR) plan

tofu-apply: build-lambdas
	$(TOFU) -chdir=$(TOFU_DIR) apply
