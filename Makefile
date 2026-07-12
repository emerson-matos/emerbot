GO      ?= go
TOFU    ?= tofu
NPM     ?= npm
COMPOSE ?= podman compose

# On NixOS / is nearly full; redirect buildah temp files to /home.
export TMPDIR := $(HOME)/.tmp/buildah
$(shell mkdir -p $(TMPDIR))

.PHONY: build test fmt lint \
        run-webhook run-api run-cli run-lambda \
        up down up-infra \
        logs-webhook logs-api \
        seed demo \
        web-dev \
        tofu-fmt tofu-fmt-check tofu-init tofu-plan tofu-apply

# ---------------------------------------------------------------------------
# Go
# ---------------------------------------------------------------------------
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

run-api:
	$(GO) run ./apps/dashboard-api/cmd/local

run-cli:
	$(GO) run ./apps/cli/cmd/cli --help

run-lambda:
	$(GO) run ./apps/webhook/cmd/lambda

# ---------------------------------------------------------------------------
# Docker Compose — local stack
# ---------------------------------------------------------------------------

## Start the full local stack (infra + apps + frontend).
## GEMINI_API_KEY must be set in .env or the environment.
up:
	$(COMPOSE) up --build

## Stop all containers and remove them.
down:
	$(COMPOSE) down

## Start only infrastructure (DynamoDB + admin). Useful for native Go dev.
up-infra:
	$(COMPOSE) up --build dynamodb-local dynamodb-admin dynamodb-init

logs-webhook:
	$(COMPOSE) logs -f webhook

logs-api:
	$(COMPOSE) logs -f dashboard-api

# ---------------------------------------------------------------------------
# Demo seed
# ---------------------------------------------------------------------------

## Seed 3 months of realistic pharmacy data into DynamoDB Local.
## Requires: $(COMPOSE) up-infra (DynamoDB must be running on :8000).
seed:
	AWS_ACCESS_KEY_ID=local AWS_SECRET_ACCESS_KEY=local AWS_REGION=us-east-1 \
	$(GO) run ./scripts/seed \
		--endpoint http://localhost:8000 \
		--table emerbot-local-financial-entries \
		--user-id pai \
		--months 3

## One command to start everything and seed demo data.
## Usage: make demo GEMINI_API_KEY=your-key
demo: up
	@echo "Waiting for dashboard-api to be healthy..."
	@until wget -qO-  http://localhost:8081/health > /dev/null 2>&1; do sleep 2; done
	$(MAKE) seed
	@echo ""
	@echo "✅ Demo ready!"
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

tofu-init:
	$(TOFU) -chdir=infra/opentofu/environments/dev init

tofu-plan:
	$(TOFU) -chdir=infra/opentofu/environments/dev plan

tofu-apply:
	$(TOFU) -chdir=infra/opentofu/environments/dev apply
