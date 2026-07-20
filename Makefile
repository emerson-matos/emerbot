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
        seed demo \
        web-dev \
        build-lambda-webhook build-lambda-dashboard-api build-lambda-notifier build-lambdas clean-lambdas \
        tofu-fmt tofu-fmt-check tofu-init tofu-plan tofu-apply tofu-destroy

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
# Create one user in the deployed Cognito user pool. Password is generated and
# printed once unless PASSWORD is supplied. PHONE (E.164, e.g. +5511999999999)
# is prep for the WhatsApp bot's phone->account linking — not yet read by any
# app code, just stored on the Cognito user.
#   make create-user EMAIL=someone@example.com [NAME="Someone"] [PHONE=+5511999999999] [PASSWORD=...]
create-user:
	@test -n "$(EMAIL)" || { echo "EMAIL is required: make create-user EMAIL=you@example.com"; exit 1; }
	eval "$$(aws configure export-credentials --format env)" && \
	POOL_ID=$$($(TOFU) -chdir=$(TOFU_DIR) output -raw cognito_user_pool_id) && \
	PASSWORD="$(if $(PASSWORD),$(PASSWORD),$$(openssl rand -base64 12))" && \
	aws cognito-idp admin-create-user --user-pool-id "$$POOL_ID" --username "$(EMAIL)" \
		--user-attributes Name=email,Value="$(EMAIL)" Name=email_verified,Value=true $(if $(NAME),Name=name$(comma)Value="$(NAME)") $(if $(PHONE),Name=phone_number$(comma)Value="$(PHONE)") \
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

tofu-plan: build-lambdas
	eval "$$(aws configure export-credentials --format env)" && \
	$(TOFU) -chdir=$(TOFU_DIR) plan

tofu-apply: build-lambdas
	eval "$$(aws configure export-credentials --format env)" && \
	$(TOFU) -chdir=$(TOFU_DIR) apply

tofu-destroy:
	eval "$$(aws configure export-credentials --format env)" && \
	$(TOFU) -chdir=$(TOFU_DIR) destroy
