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
        tofu-fmt tofu-fmt-check tofu-init tofu-plan tofu-apply tofu-destroy

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
	GOOS=linux GOARCH=arm64 $(GO) build -o $(LAMBDA_DIR)/webhook-bin ./apps/webhook/cmd/lambda
	cd $(LAMBDA_DIR) && mv webhook-bin bootstrap && zip webhook.zip bootstrap && rm bootstrap

$(DASHBOARD_ZIP):
	mkdir -p $(LAMBDA_DIR)
	GOOS=linux GOARCH=arm64 $(GO) build -o $(LAMBDA_DIR)/dashboard-bin ./apps/dashboard-api/cmd/lambda
	cd $(LAMBDA_DIR) && mv dashboard-bin bootstrap && zip dashboard-api.zip bootstrap && rm bootstrap

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
	@echo "   Login:           demo@user.com / fake123"

# ---------------------------------------------------------------------------
# Users (dashboard auth)
# ---------------------------------------------------------------------------
# Create one user in the deployed dev users table. Password is generated and
# printed once unless PASSWORD is supplied.
#   make create-user EMAIL=someone@example.com [NAME="Someone"] [PASSWORD=...]
create-user:
	@test -n "$(EMAIL)" || { echo "EMAIL is required: make create-user EMAIL=you@example.com"; exit 1; }
	eval "$$(aws configure export-credentials --format env)" && \
	USERS_TABLE=emerbot-dev-users \
	REFRESH_TOKENS_TABLE=emerbot-dev-refresh-tokens \
	$(GO) run ./scripts/create-user -email "$(EMAIL)" -name "$(NAME)" $(if $(PASSWORD),-password "$(PASSWORD)",)

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

TF_VAR_webhook_secret              ?= $(WEBHOOK_SECRET)
export TF_VAR_webhook_secret
TF_VAR_gemini_api_key_value        ?= $(GEMINI_API_KEY)
export TF_VAR_gemini_api_key_value
TF_VAR_cloudflare_account_id       ?= $(CLOUDFLARE_ACCOUNT_ID)
export TF_VAR_cloudflare_account_id
TF_VAR_meta_graph_api_token_value  ?= $(META_GRAPH_API_TOKEN)
export TF_VAR_meta_graph_api_token_value
TF_VAR_cloudflare_zone_id          ?= $(CLOUDFLARE_ZONE_ID)
export TF_VAR_cloudflare_zone_id

tofu-init: build-lambdas
	$(TOFU) -chdir=$(TOFU_DIR) init

tofu-plan: build-lambdas
	eval "$$(aws configure export-credentials --format env)" && \
	$(TOFU) -chdir=$(TOFU_DIR) plan

tofu-apply: build-lambdas
	eval "$$(aws configure export-credentials --format env)" && \
	$(TOFU) -chdir=$(TOFU_DIR) apply

tofu-destroy:
	eval "$$(aws configure export-credentials --format env)" && \
	$(TOFU) -chdir=$(TOFU_DIR) destroy
