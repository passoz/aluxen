# Aluxen em Go — contrato de arquitetura do port

> Documento normativo do branch `devgo`. Todo agente que implementa qualquer parte
> deste port segue este contrato. Ele congela layout, camadas, nomes, portas de
> domínio e convenções de teste. Divergir exige decisão registrada no Work do `pwn`.

## 1. Objetivo

Paridade funcional total com o app Bun/Hono/React SSR (33 rotas, 6 abas do painel,
portal do locatário, validação pública, PDFs de recibo e contrato, auditoria,
WhatsApp) na stack Go de `github.com/passoz/staxen/stacks/go.md`, mais as
funcionalidades novas descritas em `ESTRATEGIA.md` e `ARQUITETURA-DEPLOY.md`.

Regra dura: **o uso atual não muda** (contrato residencial, recibo mensal, baixa,
recibo em PDF, portal, auditoria). Nada é renomeado, nenhum campo novo é
obrigatório. As adições são opt-in e aparecem só onde fazem sentido.

## 2. Stack e ferramentas

| Função | Escolha |
|---|---|
| Runtime | Go 1.27 (`go.mod` em `github.com/passoz/aluxen`) |
| HTTP | `net/http` com `http.ServeMux` (padrões `GET /rota/{id}`) |
| UI | `html/template` + JS vanilla (sem framework SPA) |
| Banco | SQLite (`modernc.org/sqlite`) ou Postgres (`pgx/v5` via `database/sql`) |
| SQL | `sqlc` — o SQL vive em `queries/queries.sql` e `schema/*.sql`; nenhum SQL em Go |
| Migrações | `goose` (SQL em `migrations/sqlite` e `migrations/postgres`) |
| Validação | `github.com/go-playground/validator/v10` na borda + regras no domínio |
| Config | `github.com/caarlos0/env/v11` |
| Log | `log/slog` (JSON em produção, texto em desenvolvimento) |
| IDs | UUIDv7 (`github.com/google/uuid`) |
| PDF | `github.com/go-pdf/fpdf` |
| QR | `github.com/skip2/go-qrcode` |
| Senhas | `golang.org/x/crypto/bcrypt` (custo 10 — compatível com os hashes existentes) |
| Testes | `testing` + `github.com/stretchr/testify` |

Comandos:

```bash
go build ./...            # compila
go vet ./...              # análise estática
go test ./...             # suíte completa
go test ./... -race -coverprofile=coverage.out
sqlc generate -f sqlc.sqlite.yaml && sqlc generate -f sqlc.postgres.yaml
```

## 3. Layout

```
cmd/app/main.go                     # composition root (único lugar que conhece implementações)
cmd/migrate/main.go                 # CLI de migração SQLite → Postgres
cmd/provision/main.go               # provisionamento de instância (Easypanel) e seed de demo
internal/domain/                    # entidades, enums, regras puras, portas, erros
internal/domain/format/             # formatação de apresentação (datas, moeda, máscaras)
internal/application/               # casos de uso (dependem só de domain)
internal/infrastructure/config/     # configuração por ambiente
internal/infrastructure/logging/    # slog estruturado
internal/infrastructure/platform/   # clock, gerador de id, hasher de senha
internal/infrastructure/buildinfo/  # identificação do build em execução
internal/infrastructure/db/         # conexão, migrações, seeds, adapters e paridade
internal/infrastructure/db/sqlite/  # adapter + código gerado pelo sqlc
internal/infrastructure/db/postgres/
internal/infrastructure/db/parity/  # teste de paridade de schema (enforced)
internal/infrastructure/web/        # handlers, middlewares, sessão, CSRF, render
internal/infrastructure/web/templates/
internal/infrastructure/web/static/
internal/infrastructure/pdf/        # recibo, contrato, vistoria, recibo de devolução
internal/infrastructure/authz/      # token HMAC de autenticidade, QR, código AUTH
internal/infrastructure/whatsapp/   # cliente WAHA, jobs de envio, webhook, agente
internal/infrastructure/ai/         # extração por visão (comprovante) e por documento
internal/infrastructure/easypanel/  # cliente da API de provisionamento
schema/sqlite.sql                   # DDL canônico SQLite (fonte do sqlc)
schema/postgres.sql                 # DDL canônico Postgres (paridade enforced)
queries/queries.sql                 # queries compartilhadas pelos dois dialetos (ASCII puro)
migrations/sqlite/, migrations/postgres/
```

**Regra de dependência:** `domain` não importa `application` nem `infrastructure`.
`application` importa `domain`. `infrastructure` importa `domain` e `application`.
`cmd` importa tudo. Handler é adaptador fino: decodifica HTTP, chama caso de uso,
codifica resposta — **nenhuma regra de negócio em handler**, **nenhum SQL em Go**.

## 4. Portas de domínio (congeladas)

```go
// internal/domain/ports.go — repositórios
type UserRepository interface {
    ByID(ctx context.Context, id string) (*User, error)
    ByUsername(ctx context.Context, username string) (*User, error)
    Create(ctx context.Context, user *User) error
    UpdateUsername(ctx context.Context, id, username string) error
    UpdatePassword(ctx context.Context, id, passwordHash, changedAt string) error
}
type TenantRepository interface {
    List(ctx) ([]Tenant, error); ByID(ctx, id) (*Tenant, error); ByCPF(ctx, digits string) (*Tenant, error)
    Create(ctx, *Tenant) error; Update(ctx, *Tenant) error
    UpdatePassword(ctx, id, passwordHash, changedAt, updatedAt string) error
    Delete(ctx, id) error; Count(ctx) (int, error)
}
type PropertyRepository interface {
    List(ctx) ([]Property, error); ByID(ctx, id) (*Property, error)
    Create(ctx, *Property) error; Update(ctx, *Property) error; Delete(ctx, id) error; Count(ctx) (int, error)
}
type LeaseRepository interface {
    List(ctx) ([]Lease, error); ByID(ctx, id) (*Lease, error)
    ByTenant(ctx, tenantID string) ([]Lease, error); ByProperty(ctx, propertyID string) ([]Lease, error)
    Overlapping(ctx, propertyID, excludeID, startDate, endDate string) ([]Lease, error)
    ActiveInRange(ctx, startDate, endDate string) ([]Lease, error)
    Create(ctx, *Lease) error; Update(ctx, *Lease) error
    UpdateStatus(ctx, id string, status LeaseStatus, updatedAt string) error
    Delete(ctx, id) error; Count(ctx) (int, error); CountByStatus(ctx, status LeaseStatus) (int, error)
}
type ReceiptRepository interface {
    List(ctx) ([]Receipt, error); ByID(ctx, id) (*Receipt, error)
    ByTenant(ctx, tenantID string) ([]Receipt, error); ByLease(ctx, leaseID string) ([]Receipt, error)
    Create(ctx, *Receipt) error; CreateIfAbsent(ctx, *Receipt) (int64, error)
    Update(ctx, *Receipt) error
    UpdatePayment(ctx, id, paidAt string, paidAmount float64, updatedAt string) error
    Delete(ctx, id) error; Count(ctx) (int, error)
}
type SettingRepository interface {
    Get(ctx, key string) (string, error); List(ctx) (map[string]string, error); Set(ctx, key, value string) error
}
type AuditRepository interface {
    Insert(ctx, *AuditLog) error; List(ctx, AuditFilter) ([]AuditLog, error)
    Count(ctx, AuditFilter) (int, error); Actors(ctx) ([]string, error)
}

// portas de serviço
type Clock interface{ Now() time.Time }
type IDGenerator interface{ New() string }
type PasswordHasher interface{ Hash(string) (string, error); Compare(hash, password string) error }
```

Entidades em `internal/domain/entities.go` (campos anuláveis são ponteiros),
enums em `enums.go`, regras puras em `rules.go` (`Overlaps`, `ReceiptStatusOf`,
`DueDateForMonth`, `GeneratedKeyForMonth`, `ValidateEmail`, …), valor por extenso
em `money_words.go`, catálogo de auditoria em `audit_catalog.go` e o diff
campo-a-campo em `audit_fields.go`.

Erros: `domain.ErrNotFound`, `ErrConflict`, `ErrUnauthorized` e
`domain.Invalid("mensagem")` para validação com mensagem de usuário.

## 5. Banco: dois dialetos, um comportamento

- O dialeto é resolvido no boot: `DATABASE_URL=file:/data/aluxen.db` → SQLite;
  `postgres://…` → Postgres. Sem `DATABASE_URL`, usa `DB_DIR/aluxen.db`.
- `queries/queries.sql` é **um único arquivo ASCII** usado pelos dois configs do
  sqlc (o parser do engine sqlite do sqlc quebra com caractere multi-byte).
- `schema/sqlite.sql` e `schema/postgres.sql` têm paridade obrigatória de tabelas,
  colunas, nulidade, PK, FK (com ação de delete) e índices. O mapa de tipos é
  `text→text`, `integer→integer`, `real→double precision`. Divergência quebra o
  teste em `internal/infrastructure/db/parity`.
- Timestamps são escritos pelo código, nunca por default de banco:
  - `created_at`/`updated_at` de cadastros: `format.FormatUTCStamp` (UTC, `YYYY-MM-DD HH:MM:SS`, igual ao `datetime('now')` do SQLite);
  - `paid_at`: `format.FormatLocalDateTime` (local, `YYYY-MM-DDTHH:mm`);
  - `audit_logs.created_at`: `format.FormatLocalDateTimeMillis` (local, com milissegundos).
- Listas de ids são percorridas em transação pela aplicação (nada de `IN (...)` dinâmico).

## 6. Web: sessão, CSRF e travas

- Cookies com os mesmos nomes do app atual: `__aluxen_session` (admin) e
  `__aluxen_client_session` (locatário). `HttpOnly`, `SameSite=Lax`, `Path=/`,
  30 dias, `Secure` quando `COOKIE_SECURE=true` ou `APP_ENV=production`.
  **Diferença deliberada:** o valor é assinado com HMAC-SHA256 usando
  `SESSION_SECRET` (`<id>.<assinatura>`); o app antigo guardava o id cru, o que
  permitia forjar sessão. Assinatura é invisível ao usuário e corrige a falha.
- CSRF: double-submit assinado (`crypto/rand` + `HMAC-SHA256`), cookie
  `csrf_token` + campo oculto `csrf_token`, comparação com `hmac.Equal`. Obrigatório
  em todo método mutante, exceto `/api/whatsapp/webhook` (assinatura própria).
- Rate limit de login: janela fixa de 60s, 5 tentativas por IP
  (`RATE_LIMIT_MAX`), 60 globais (`RATE_LIMIT_GLOBAL_MAX`), bloqueio progressivo a
  partir de 10 falhas (`LOGIN_LOCK_THRESHOLD`) com backoff `5min * 2^strikes`
  limitado a 60min, header `Retry-After`. Vale para `/login` e `/portal/login`.
- Trava de senha padrão: com `mustChangePassword`, tudo redireciona para
  `/settings?forced=1` (ou `403` em `/api/*`), exceto `/login`, `/logout`,
  `/settings`, `/api/health`, `/portal*`, `/validar*` e estáticos.
- Logs HTTP: `requestId` de 8 caracteres, método, rota, status, duração, IP.

## 7. Convenções de código

- `internal/infrastructure/web` expõe `Handlers` com dependências injetadas; rotas
  registradas em `router.go` com padrões do `ServeMux`.
- Templates em `internal/infrastructure/web/templates`, embutidos com
  `//go:embed`. Estáticos em `internal/infrastructure/web/static`, também embutidos.
- Nada de `fmt.Println`/`log.Printf`: `slog` estruturado, sem senha/token em log.
- Erros: `fmt.Errorf("contexto: %w", err)`; nunca `nil, nil`.
- Testes: unitário com dublês das portas, integração com SQLite `:memory:` (e
  Postgres quando `TEST_DATABASE_URL` existir, senão `t.Skip`), e2e com
  `httptest.NewServer`.
- Nomes e mensagens de usuário em português, iguais aos do app atual.

## 8. Mapa de rotas (paridade obrigatória)

| Rota (método + path) | Handler Go | Observação |
|---|---|---|
| `GET /api/health` | `Handlers.Health` | `{status,runtime,framework,database,orm,timestamp}` — runtime `go`, framework `net/http`, database `sqlite (modernc)` ou `postgres (pgx)`, orm `sqlc` |
| `GET /api/info` | `Handlers.SystemInfo` | sessão admin; runtime/uptime/caminho+tamanho do banco/contagens/build |
| `GET /` | `Handlers.Root` | logado → `/dashboard?tab=dashboard`, senão `/login` |
| `GET /login` | `Handlers.LoginPage` | já logado → `/dashboard?tab=dashboard` |
| `POST /login` | `Handlers.Login` | 302 / 400 / 401 / 429 + `Retry-After` |
| `POST|GET /logout` | `Handlers.Logout` | limpa cookie, audita, 302 `/login` |
| `GET /validar/recibo/{id}` | `Handlers.VerifyReceipt` | token HMAC; 400 inválido, 404 inexistente, 403 não pago |
| `GET /validar/contrato/{id}` | `Handlers.VerifyContract` | token HMAC; 400 inválido, 404 inexistente |
| `GET /portal/login` | `Handlers.PortalLoginPage` | já logado → `/portal` |
| `POST /portal/login` | `Handlers.PortalLogin` | rate limit; 302 `/portal/password` ou `/portal` |
| `GET|POST /portal/logout` | `Handlers.PortalLogout` | 302 `/portal/login` |
| `GET /portal` | `Handlers.PortalHome` | sessão locatário; sem ela 302 `/portal/login` |
| `GET /portal/password` | `Handlers.PortalPasswordPage` | sessão locatário |
| `POST /portal/password` | `Handlers.PortalPassword` | 302 `/portal?ok=senha` (a tela deve reconhecer `ok=senha` — corrige o bug do app atual, que redireciona `ok=password`) |
| `GET /dashboard` | `Handlers.Dashboard` | abas `dashboard,tenants,properties,leases,receipts,history`; `json=true` devolve JSON |
| `POST /dashboard` | `Handlers.DashboardMutation` | intents abaixo |
| `GET /settings` | `Handlers.SettingsPage` | isenta da trava de senha |
| `POST /settings` | `Handlers.SettingsMutation` | intents `save-landlord`, `save-app-url`, `change-password` |
| `GET /metrics` | `Handlers.Metrics` | JSON de métricas |
| `GET /audit` | `Handlers.AuditList` | JSON paginado com filtros |
| `GET /audit/export` | `Handlers.AuditExport` | CSV (delimitador `;`, BOM, 13 colunas, limite 5000) |
| `GET /export` | `Handlers.BackupExport` | JSON `{exportedAt,tenants,properties,leases,receipts}` |
| `POST /import` | `Handlers.BackupImport` | multipart, campo `file`; 302 `/dashboard?tab=dashboard&toast=importado` |
| `GET /pdf` | `Handlers.ReceiptPDF` | `id`, `token`, `assinatura`; admin, token HMAC ou locatário titular |
| `GET /contract-pdf` | `Handlers.ContractPDF` | `id`, `token`; admin, token HMAC ou locatário titular |
| `GET /api/whatsapp/status` | `Handlers.WhatsAppStatus` | sessão admin |
| `POST /api/whatsapp/login/start` | `Handlers.WhatsAppLoginStart` | |
| `GET /api/whatsapp/login/status` | `Handlers.WhatsAppLoginStatus` | |
| `POST /api/whatsapp/login/cancel` | `Handlers.WhatsAppLoginCancel` | |
| `POST /api/whatsapp/logout` | `Handlers.WhatsAppLogout` | |
| `POST /api/whatsapp/preview` | `Handlers.WhatsAppPreview` | `type=receipt\|lease`, `id` |
| `POST /api/whatsapp/send` | `Handlers.WhatsAppSend` | devolve `jobId` |
| `GET /api/whatsapp/send/status` | `Handlers.WhatsAppSendStatus` | `jobId` |
| `POST /api/whatsapp/webhook` | `Handlers.WhatsAppWebhook` | **novo** — WAHA; isento de CSRF, valida HMAC do webhook |

### Intents de `POST /dashboard`

`tenant.create`, `tenant.update`, `tenant.delete`, `property.create`,
`property.update`, `property.delete`, `lease.create`, `lease.update`,
`lease.terminate`, `lease.suspend`, `lease.reactivate`, `lease.delete`,
`receipt.create`, `receipt.update`, `receipt.pay`, `receipt.delete`,
`receipt.batch-pay`, `receipt.batch-delete`, `receipt.generate-monthly`.
Resposta: `{"ok":true}` ou `400 {"error":"mensagem"}`; `401 {"error":"Sessão expirada."}`.

### Intents de `POST /settings`

`save-landlord` (`name`, `cpf`, `email`), `save-app-url` (`app_url`),
`change-password` (`current_password`, `new_password`, `confirm_password`).

## 9. Funcionalidades novas (Works seguintes)

| Área | Entrega |
|---|---|
| Temporada | caução com recibo de devolução, auto de vistoria com fotos e PDF, calendário de ocupação, categoria de taxa de limpeza/consumíveis |
| IA | baixa automática por leitura de comprovante (visão → JSON tipado → validação determinística → baixa ou fila de revisão), onboarding por documento (contrato antigo, RG/CNH), auditoria da extração |
| WhatsApp bidirecional | WAHA com webhook, mensagens recebidas persistidas e vinculadas ao inquilino, agente de cobrança com ferramentas tipadas e limitadas, régua de cobrança adaptativa |
| Conformidade | assinatura "visualizada" (sem promessa de ICP-Brasil), LGPD (aviso e operador de IA), modos demo/cliente do WhatsApp |
| Deploy | provisionamento por instância no Easypanel, seed de demo, expiração/remoção de demos, CLI de migração SQLite→Postgres, reajuste assistido IGP-M/IPCA, relatório em linguagem natural por tool-calling tipado |

Regra herdada: **nunca dar baixa por saída de LLM** — a IA lê o documento, o
código decide. Toda decisão automática precisa de validação determinística e
registro na trilha de auditoria.
