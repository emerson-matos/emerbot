---
name: demo
description: Bring up the full local emerbot stack (podman compose) and seed demo data. Use when the user wants to run the app locally, spin up the demo, or test end-to-end against local services.
---

Start the local emerbot environment for hands-on testing.

Steps:

1. Run `make demo`. This runs `podman compose up`, waits on `dashboard-api` at `http://localhost:8081/health`, then seeds ~120 demo financial entries.
2. Once healthy, report the service URLs and login:
   - Web dashboard: http://localhost:5173
   - Dashboard API: http://localhost:8081
   - Webhook: http://localhost:8080
   - WhatsApp simulator: http://localhost:9000
   - DynamoDB admin: http://localhost:8001
   - Demo login: `demo@user.com` / `fake123`
3. If containers fail to start, check whether `TMPDIR` is set to `$HOME/.tmp/buildah` (podman stages layers there on this machine) and whether ports are already in use.

To tear down afterward: `make down` (or `podman compose down`).
