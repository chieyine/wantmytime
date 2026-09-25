# WantMyTime (wantmytime.com)

> **Sell your time with one link. Buyers pay upfront; you get paid after the call.**

WantMyTime is a dedicated booking and payment platform for professionals and creators. Share one personal link where people pick an available slot, pay upfront, and meet privately. WantMyTime holds the payment, gives both sides a 2-hour problem window after the call, then pays the seller out about 3 hours after it ends.

Payments and payouts run on Kora. Nigeria (NGN: bank transfer, pay with bank) is live first; Ghana (GHS) and Kenya (KES), both by mobile money, are built in and switch on per country with `SELLER_COUNTRIES` once Kora enables them. A seller's country fixes the currency they charge in. WantMyTime does not take card payments. See [docs/FLOW_WALKTHROUGH.md](docs/FLOW_WALKTHROUGH.md) and [docs/LAUNCH_CHECKLIST.md](docs/LAUNCH_CHECKLIST.md).

---

## 🏗 Architecture

WantMyTime is composed of two primary services:

- **`apps/web`**: SvelteKit web application with full SSR and static pre-rendering, styled with bespoke typographic design, client-side validation, and operator dashboards.
- **`services/core`**: High-performance Go API with strict security controls, PostgreSQL persistence, idempotency locks, Google Calendar sync, encrypted secrets, and fail-closed payment rails.

```
                  ┌──────────────────────┐
                  │      wantmytime.com   │
                  │   Reverse Proxy/CDN  │
                  └──────────┬───────────┘
                             │
              ┌──────────────┴──────────────┐
              ▼                             ▼
    ┌──────────────────┐          ┌──────────────────┐
    │     apps/web     │          │  services/core   │
    │  (SvelteKit SSR) │ ◄──────► │     (Go API)     │
    └──────────────────┘          └─────────┬────────┘
                                            │
                                  ┌─────────┴────────┐
                                  ▼                  ▼
                         ┌─────────────────┐ ┌───────────────┐
                         │   PostgreSQL    │ │ Kora Payments │
                         │  (Persistence)  │ │ (Bank/MoMo)   │
                         └─────────────────┘ └───────────────┘
```

---

## ⚡ Quick Start

### Prerequisites
- Node.js 20+
- Go 1.24+
- PostgreSQL 17+ (or Docker Compose)

### 1. Configure Environment
```bash
cp .env.example .env
```

### 2. Run Database & Migrations
```bash
# Start local postgres using docker compose
docker compose up -d db

# Apply migrations
cd services/core
go run ./cmd/api migrate
```

### 3. Run Backend API
```bash
cd services/core
DATABASE_URL="postgres://aside:aside@127.0.0.1:54329/aside?sslmode=disable" LOCAL_PAYMENT_SIMULATOR="true" go run ./cmd/api
```

### 4. Run Frontend Web
```bash
cd apps/web
npm install
npm run dev
```

Visit [http://127.0.0.1:5173](http://127.0.0.1:5173) in your browser.

---

## 🧪 Testing & Verification

Run the full verification test suite across both frontend and backend:

```bash
# Go unit & integration tests with race detector
cd services/core
go test -race ./...

# SvelteKit typecheck and production build
cd ../../apps/web
npm run check
npm run build
```

---

## 🔒 Security & Safety Guarantees

- **Guaranteed Payout Delays**: Payouts to sellers are held until ~3 hours after the call ends and the buyer problem window has passed.
- **Fail-Closed Financial Gates**: Live payment collection requires explicit provider approval flags and approval ID verification.
- **No card payments**: buyers pay by bank transfer, pay with bank or mobile money through Kora. Sellers' bank account numbers are AES-GCM encrypted at rest.
- **TOTP Replay Protection**: Operator administrative actions enforce strict time-step replay tracking and audited logs.
- **Idempotent Webhooks & Allocations**: Webhooks from payment gateways are strictly de-duplicated with advisory locks to prevent double-crediting.

---

## 📜 License

Private & Proprietary. All rights reserved.
