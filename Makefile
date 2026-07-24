GO      ?= go
TOFU    ?= tofu
NPM     ?= npm
COMPOSE ?= podman compose

# $(if ...) splits its arguments on every top-level comma, so a literal comma
# inside a then/else branch (e.g. "Name=x,Value=y") gets misparsed as an
# extra argument instead of staying part of the text — this defers the comma
# until after argument-splitting has already happened.
comma := ,

# On NixOS / is nearly full; redirect buildah temp files to /home.
export TMPDIR := $(HOME)/.tmp/buildah
$(shell mkdir -p $(TMPDIR))

TOFU_DIR := infra/opentofu/environments/dev
BOOTSTRAP_DIR := infra/opentofu/bootstrap
LAMBDA_DIR := $(TOFU_DIR)/.lambdas
LAMBDA_ZIP := $(LAMBDA_DIR)/webhook.zip
DASHBOARD_ZIP := $(LAMBDA_DIR)/dashboard-api.zip
NOTIFIER_ZIP := $(LAMBDA_DIR)/notifier.zip

# Every Lambda links against the shared packages/, so any Go change can affect
# any zip. We over-approximate each zip's dependency set to all non-test Go
# sources plus the module files and let Make decide when a zip is stale — this
# is what removes the old "remember to rm the zips before apply" footgun.
GO_SOURCES := $(shell find apps packages -name '*.go' ! -name '*_test.go') go.mod go.sum

.PHONY: build test fmt lint \
        run-webhook run-api run-cli run-lambda \
        up down up-infra \
        logs-webhook logs-api \
        seed demo demo-ollama \
        web-dev \
        build-lambda-webhook build-lambda-dashboard-api build-lambda-notifier build-lambdas clean-lambdas \
        tofu-fmt tofu-fmt-check tofu-init tofu-bootstrap tofu-migrate-state gh-secrets \
        tofu-plan tofu-apply tofu-destroy

# ---------------------------------------------------------------------------
# Go
# ---------------------------------------------------------------------------
build:
	$(GO) build ./...

# ---------------------------------------------------------------------------
# Lambda zips (Go -> Linux/arm64 -> a single `bootstrap` inside the zip)
#
# Each zip lists $(GO_SOURCES) as a prerequisite, so `make build-lambdas` (and
# therefore tofu-plan/tofu-apply, which depend on it) rebuilds a zip whenever a
# Go source is newer than it — no more deleting zips by hand to force a fresh
# build. Builds are reproducible (-trimpath, CGO off, zeroed mtime, `zip -X`),
# so a rebuild that produces a byte-identical binary keeps the same
# source_code_hash and Tofu shows no diff for that Lambda.
#
# Staged in a per-Lambda .build dir (never the shared $(LAMBDA_DIR)) so parallel
# `make -j` runs don't fight over one `bootstrap` file.
#   $(1) = Lambda name (produces $(LAMBDA_DIR)/$(1).zip)
#   $(2) = Go package to compile
define build_lambda
@mkdir -p $(LAMBDA_DIR)/$(1).build
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -o $(LAMBDA_DIR)/$(1).build/bootstrap $(2)
cd $(LAMBDA_DIR)/$(1).build && touch -d @0 bootstrap && rm -f ../$(1).zip && zip -X -q ../$(1).zip bootstrap
@rm -rf $(LAMBDA_DIR)/$(1).build
endef

$(LAMBDA_ZIP): $(GO_SOURCES)
	$(call build_lambda,webhook,./apps/webhook/cmd/lambda)

$(DASHBOARD_ZIP): $(GO_SOURCES)
	$(call build_lambda,dashboard-api,./apps/dashboard-api/cmd/lambda)

$(NOTIFIER_ZIP): $(GO_SOURCES)
	$(call build_lambda,notifier,./apps/notifier/cmd/lambda)

build-lambda-webhook: $(LAMBDA_ZIP)
build-lambda-dashboard-api: $(DASHBOARD_ZIP)
build-lambda-notifier: $(NOTIFIER_ZIP)
build-lambdas: build-lambda-webhook build-lambda-dashboard-api build-lambda-notifier

# Escape hatch only — the zips now rebuild themselves when Go sources change.
clean-lambdas:
	rm -rf $(LAMBDA_DIR)

# ---------------------------------------------------------------------------
# Test / fmt / lint
# ---------------------------------------------------------------------------
test:
	$(GO) test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1 && rm -f coverage.out

fmt:
	npm --prefix apps/web run lint -- --fix
	gofumpt -w .

lint:
	npm --prefix apps/web run lint
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
# COMPOSE_EXTRA lets demo-ollama layer the optional Ollama stack (ADR-012) on top
# of the base compose files without duplicating recipes.
COMPOSE_EXTRA ?=

up:
	$(COMPOSE) $(COMPOSE_EXTRA) up --build

down:
	$(COMPOSE) $(COMPOSE_EXTRA) down

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

# Same as `demo`, but layers the local Ollama LLM (ADR-012) so natural-language
# chat hits a real open-source model instead of the StaticClient. First run pulls
# ~5GB and CPU inference is slow. Override the model with OLLAMA_MODEL=qwen2.5:7b.
# Tear down with: make down COMPOSE_EXTRA="-f docker-compose.yml -f docker-compose.ollama.yml"
demo-ollama:
	$(MAKE) demo COMPOSE_EXTRA="-f docker-compose.yml -f docker-compose.ollama.yml"

# ---------------------------------------------------------------------------
# Users (dashboard auth)
# ---------------------------------------------------------------------------
# Create one user in the deployed Cognito user pool. Password is generated and
# printed once unless PASSWORD is supplied. PHONE (E.164, e.g. +5511999999999)
# is required — it's the number the dashboard uses for WhatsApp alerts (see
# packages/domain/notifications.go) and the pool's schema rejects users
# without one.
#   make create-user EMAIL=someone@example.com PHONE=+5511999999999 [NAME="Someone"] [PASSWORD=...]
create-user:
	@test -n "$(EMAIL)" || { echo "EMAIL is required: make create-user EMAIL=you@example.com PHONE=+5511999999999"; exit 1; }
	@test -n "$(PHONE)" || { echo "PHONE is required: make create-user EMAIL=you@example.com PHONE=+5511999999999"; exit 1; }
	eval "$$(aws configure export-credentials --format env)" && \
	POOL_ID=$$($(TOFU) -chdir=$(TOFU_DIR) output -raw cognito_user_pool_id) && \
	PASSWORD="$(if $(PASSWORD),$(PASSWORD),$$(openssl rand -base64 12))" && \
	aws cognito-idp admin-create-user --user-pool-id "$$POOL_ID" --username "$(EMAIL)" \
		--user-attributes Name=email,Value="$(EMAIL)" Name=email_verified,Value=true Name=phone_number,Value="$(PHONE)" $(if $(NAME),Name=name$(comma)Value="$(NAME)") \
		--message-action SUPPRESS && \
	aws cognito-idp admin-set-user-password --user-pool-id "$$POOL_ID" --username "$(EMAIL)" \
		--password "$$PASSWORD" --permanent && \
	echo "Created user $(EMAIL) — password: $$PASSWORD"

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
TF_VAR_webhook_secret_value        ?= $(WEBHOOK_VERIFY_TOKEN)
export TF_VAR_webhook_secret_value
TF_VAR_gemini_api_key_value        ?= $(GEMINI_API_KEY)
export TF_VAR_gemini_api_key_value
TF_VAR_cloudflare_account_id       ?= $(CLOUDFLARE_ACCOUNT_ID)
export TF_VAR_cloudflare_account_id
TF_VAR_meta_graph_api_token_value  ?= $(META_GRAPH_API_TOKEN)
export TF_VAR_meta_graph_api_token_value
TF_VAR_whatsapp_phone_number_id    ?= $(WHATSAPP_PHONE_NUMBER_ID)
export TF_VAR_whatsapp_phone_number_id
TF_VAR_cloudflare_zone_id          ?= $(CLOUDFLARE_ZONE_ID)
export TF_VAR_cloudflare_zone_id

tofu-init: build-lambdas
	$(TOFU) -chdir=$(TOFU_DIR) init

# One-time-per-account: create the S3 state bucket + GitHub OIDC deploy role.
# Uses your local admin AWS creds. See docs/deploy.md.
tofu-bootstrap:
	eval "$$(aws configure export-credentials --format env)" && \
	$(TOFU) -chdir=$(BOOTSTRAP_DIR) init && \
	$(TOFU) -chdir=$(BOOTSTRAP_DIR) apply

# One-time: push the existing local terraform.tfstate up to the S3 backend
# (run after tofu-bootstrap, the first time you switch to remote state).
tofu-migrate-state:
	eval "$$(aws configure export-credentials --format env)" && \
	$(TOFU) -chdir=$(TOFU_DIR) init -migrate-state

# Copy the deploy secrets from your local env into GitHub Actions (needs `gh`).
# Load your values first, e.g.:  set -a && . ./.env && set +a
gh-secrets:
	./scripts/gh-secrets.sh

tofu-plan: build-lambdas
	eval "$$(aws configure export-credentials --format env)" && \
	$(TOFU) -chdir=$(TOFU_DIR) plan

tofu-apply: build-lambdas
	eval "$$(aws configure export-credentials --format env)" && \
	$(TOFU) -chdir=$(TOFU_DIR) apply

tofu-destroy:
	eval "$$(aws configure export-credentials --format env)" && \
	$(TOFU) -chdir=$(TOFU_DIR) destroy
