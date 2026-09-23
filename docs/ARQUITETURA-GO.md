# 🏛️ Aluxen em Go — Manual Completo de Arquitetura

> **Documento normativo e guia arquitetural definitivo do projeto Aluxen em Go.**
> Este documento consolida a arquitetura completa, convenções de camadas, portas de domínio, motor de banco dual-dialeto, subsistema de locação de temporada com visão computacional, inteligência de documentos, atendimento conversacional e segurança física/lógica por instância.

---

## 📑 Sumário

1. [Visão Geral & Princípios Arquiteturais](#1-visão-geral--princípios-arquiteturais)
2. [Stack Tecnológica & Racional de Decisão](#2-stack-tecnológica--racional-de-decisão)
3. [Layout Estrutural da Aplicação](#3-layout-estrutural-da-aplicação)
4. [Camada de Domínio: Entidades, Portas & Invariantes](#4-camada-de-domínio-entidades-portas--invariantes)
5. [Motor de Persistência Dual-Dialeto (SQLite & PostgreSQL)](#5-motor-de-persistência-dual-dialeto-sqlite--postgresql)
6. [Locação de Temporada & Comparador de Vistorias por Visão (Tier 2)](#6-locação-de-temporada--comparador-de-vistorias-por-visão-tier-2)
7. [Inteligência de Documentos: Baixa Automática, Intake & Reajuste](#7-inteligência-de-documentos-baixa-automática-intake--reajuste)
8. [WhatsApp Bidirecional & Agente de Cobrança / Atendimento](#8-whatsapp-bidirecional--agente-de-cobrança--atendimento)
9. [Segurança em Camadas: Sessão, CSRF, Rate Limit & Auditoria](#9-segurança-em-camadas-sessão-csrf-rate-limit--auditoria)
10. [Interface com o Usuário, Templates Nativos & PWA Offline](#10-interface-com-o-usuário-templates-nativos--pwa-offline)
11. [Catálogo Completo de Rotas HTTP](#11-catálogo-completo-de-rotas-http)
12. [Governança e Verificação Normativa](#12-governança-e-verificação-normativa)

---

## 1. Visão Geral & Princípios Arquiteturais

O **Aluxen** é um sistema de gestão de locações residenciais e de temporada, projetado como uma **aplicação monolítica modular em Go** governada pelos princípios da **Clean Architecture** (Arquitetura Limpa) e das **Portas e Adaptadores** (Arquitetura Hexagonal).

### Princípios Cardeais

```
                      +-----------------------------------+
                      |         cmd/ (Entrypoints)        |
                      |    app | migrate | provision      |
                      +-----------------+-----------------+
                                        | (Injeção de dependências)
                                        v
                      +-----------------+-----------------+
                      |    infrastructure/ (Adapters)     |
                      |  web | db | pdf | ai | whatsapp   |
                      +-----------------+-----------------+
                                        |
                                        v
                      +-----------------+-----------------+
                      |     application/ (Use Cases)      |
                      | orquestração pura das regras      |
                      +-----------------+-----------------+
                                        |
                                        v
                      +-----------------+-----------------+
                      |         domain/ (Core)            |
                      |  entidades, enums, regras, portas |
                      +-----------------------------------+
```

1. **Regra de Dependência Unidirecional e Estrita:**
   - `domain` é o núcleo absoluto. **Não importa nenhum outro pacote do projeto** e nenhuma biblioteca de infraestrutura ou framework.
   - `application` importa exclusivamente `domain`. Orquestra casos de uso consumindo as portas (interfaces) definidas no domínio.
   - `infrastructure` implementa os detalhes técnicos (banco de dados, HTTP, geração de PDFs, chamadas de IA, WhatsApp) adaptando-os para as portas exigidas por `domain` e `application`.
   - `cmd` é a raiz de composição (*composition root*), único local autorizado a instanciar implementações concretas e injetá-las na aplicação.
2. **Zero Mudança no Uso Residencial Clássico:**
   - O uso tradicional (as 5 unidades, contrato residencial padrão, geração de recibos, baixa e exportação) é 100% preservado e idêntico em comportamento.
   - Novas funcionalidades (caução, vistoria com fotos, calendário de ocupação, IA de visão) são **aditivas** e restritas a contratos configurados como temporada.
3. **Isolamento Físico por Instância:**
   - O código não implementa *multi-tenant* lógico (sem `tenant_id` espalhado em todas as tabelas).
   - Clientes e demonstrações são isolados **fisicamente**: cada cliente recebe uma instância autônoma com container, volume e banco dedicados, provisionada no Easypanel.
4. **Decisão Determinística em Automações ("A IA lê, o código decide"):**
   - Modelos de IA e Visão Computacional são operadores de leitura: extraem campos brutos e sugerem leituras.
   - Nenhuma baixa bancária, cadastro definitivo ou julgamento de dano é efetuado por saída pura de LLM. O código valida o vocabulário, confere o saldo em aberto e descarta anomalias deterministicamente.

---

## 2. Stack Tecnológica & Racional de Decisão

A seleção de tecnologias segue a especificação canônica Go de `github.com/passoz/staxen/stacks/go.md`:

| Componente | Tecnologia | Racional de Engenharia |
|---|---|---|
| **Linguagem & Runtime** | **Go 1.27** | Binário estático compilado, altíssimo desempenho, consumo mínimo de memória (< 30 MB RSS), inicialização instantânea (< 15 ms). |
| **Servidor HTTP** | **`net/http` (`http.ServeMux`)** | Roteador nativo de Go 1.22+ com suporte a métodos e curingas (`GET /validar/recibo/{id}`). Zero dependência externa de microframeworks. |
| **Renderização Web** | **`html/template` nativo** | Server-Side Rendering (SSR) nativo com escape automático contextual contra XSS, sem overhead de Node.js, V8 ou hidratação React. |
| **Bancos Suportados** | **SQLite (`modernc.org/sqlite`) & PostgreSQL (`jackc/pgx/v5`)** | SQLite sem CGO para deployments portáteis e econômicos em volume local; PostgreSQL nativo para alto volume ou redundância. |
| **Compilador SQL** | **`sqlc` (v1.31+)** | Gera código Go fortemente tipado a partir de queries SQL brutas. Sem reflexão, sem ORM pesado, sem SQL montado dinamicamente em runtime. |
| **Migrações** | **`pressly/goose/v3`** | Migrações SQL declarativas aplicadas automaticamente no boot de forma idempotente em ambos os dialetos. |
| **Geração de PDF** | **`github.com/go-pdf/fpdf`** | Criação vetorial de recibos, contratos e laudos técnicos A4 diretamente no backend, sem necessidade de navegador headless (Chromium). |
| **Código QR & Autenticidade** | **`skip2/go-qrcode` + HMAC-SHA256** | Geração de QRs embutidos e verificação de assinatura em tempo constante com segredo seguro. |
| **Hashing Criptográfico** | **`golang.org/x/crypto/bcrypt`** | Custo 10, estritamente compatível com os hashes de senha já persistidos em produção. |
| **Logging Estruturado** | **`log/slog` nativo** | JSON estruturado em produção e texto colorido em desenvolvimento, com correlação por `requestId` e filtro de segredos. |
| **Configuração** | **`github.com/caarlos0/env/v11`** | Configuração 12-factor limpa, lida diretamente de variáveis de ambiente com validação estrita no boot. |

---

## 3. Layout Estrutural da Aplicação

```
aluxen/
├── cmd/
│   ├── app/                 # Ponto de entrada do servidor web e composition root
│   ├── migrate/             # CLI autônoma de migração SQLite → PostgreSQL com dry-run
│   └── provision/           # CLI de provisionamento e ciclo de vida de instâncias Easypanel
├── internal/
│   ├── domain/              # Núcleo de negócio (entidades, enums, regras puras, interfaces de portas)
│   │   └── format/          # Formatadores de moeda (BRL), extenso, datas e máscaras
│   ├── application/         # Casos de uso da aplicação (interactors)
│   └── infrastructure/      # Adaptadores concretos
│       ├── ai/              # Cliente HTTP para leitura de comprovantes e comparador de vistorias
│       ├── authz/           # Token HMAC, verificação constante, códigos AUTH
│       ├── buildinfo/       # Metadados de versão, commit e subversão do build
│       ├── config/          # Carregamento e validação estrita de variáveis de ambiente
│       ├── db/              # Conexão de banco, migrações Goose e sementes
│       │   ├── parity/      # Testes obrigatórios de paridade de esquema SQLite vs PostgreSQL
│       │   ├── postgres/    # Adaptadores e código sqlc para PostgreSQL
│       │   ├── schema/      # DDLs compartilhados
│       │   └── sqlite/      # Adaptadores e código sqlc para SQLite
│       ├── easypanel/       # Cliente HTTP da API de infraestrutura do Easypanel
│       ├── logging/         # Handlers e sanitização de logs estruturados (slog)
│       ├── pdf/             # Construtores de layout e desenho de documentos PDF
│       ├── platform/        # Provedores de relógio (Clock), IDs (UUIDv7) e bcrypt
│       ├── web/             # Handlers HTTP, middlewares (CSRF, sessão, rate limit), rotas
│       │   ├── static/      # CSS, JS (PWA/offline), manifest e imagens embutidos
│       │   └── templates/   # Telas e parciais HTML renderizados por html/template
│       └── whatsapp/        # Integração WAHA, enfileiramento de jobs e webhook HMAC
├── migrations/              # Scripts de migração Goose (.sql)
│   ├── postgres/            # Migrações dialeto PostgreSQL
│   └── sqlite/              # Migrações dialeto SQLite
├── queries/
│   └── queries.sql          # Catálogo canônico de queries em SQL ASCII para o sqlc
├── schema/
│   ├── postgres.sql         # DDL canônico PostgreSQL
│   └── sqlite.sql           # DDL canônico SQLite
├── .air.toml                # Configuração de live-reload local com Air
├── Dockerfile               # Build multi-stage terminando em Google Distroless
├── docker-compose.yml       # Orquestração do ambiente de produção (Docker e Podman)
├── docker-compose.test.yml  # PostgreSQL para suíte de testes (exclusivamente Podman)
└── Makefile                 # Automação dos ambientes dev, test e prod
```

---

## 4. Camada de Domínio: Entidades, Portas & Invariantes

A pasta `internal/domain` encapsula as entidades de negócio e define os contratos abstratos através de interfaces Go (*ports*).

### Entidades Principais (`entities.go`)

- **`User`**: Administrador da instância (login, hash bcrypt, status de troca obrigatória de senha).
- **`Tenant`**: Locatário (nome, CPF, telefone formatado/limpo, e-mail, senha opcional de acesso ao portal).
- **`Property`**: Imóvel (endereço, cidade, estado, CEP, descrição, observações).
- **`Lease`**: Contrato de locação (locatário, imóvel, valores de aluguel e encargos, vigência, índice de reajuste, caução retida, status: `active`, `suspended`, `terminated`, `season`).
- **`Receipt`**: Lançamento financeiro mensal ou avulso (competência, vencimento, valor esperado, valor pago, data de quitação, categoria: `rent`, `extra`, `cleaning`, `consumables`, `deposit-refund`).
- **`Deposit`**: Registro de caução em contrato de temporada (valor recebido, data, status: `none`, `held`, `refunded`, `partially_refunded`, `forfeited`, devoluções parciais registradas).
- **`Inspection`**: Auto de vistoria de entrada (`checkin`) ou saída (`checkout`), com estado de conservação, data, lista de itens conferidos e galeria de fotos.
- **`AuditLog`**: Registro imutável de auditoria (ator, IP, entidade, ID, ação executada, diff antes/depois em JSON, carimbo com milissegundos).

### Portas de Repositório (`ports.go`)

Todas as operações de I/O em banco de dados são declaradas como interfaces Go:

```go
type TenantRepository interface {
    List(ctx context.Context) ([]Tenant, error)
    ByID(ctx context.Context, id string) (*Tenant, error)
    ByCPF(ctx context.Context, digits string) (*Tenant, error)
    Create(ctx context.Context, tenant *Tenant) error
    Update(ctx context.Context, tenant *Tenant) error
    Delete(ctx context.Context, id string) error
    Count(ctx context.Context) (int, error)
}
// Interfaces equivalentes para Property, Lease, Receipt, Setting, Audit, Inspection, Job, etc.
```

### Regras Puras de Negócio (`rules.go`)

- **Cálculo de Situação do Recibo (`ReceiptStatusOf`):**
  - Se `paid_at` preenchido $\rightarrow$ `paid` (verde).
  - Se `now > due_date` $\rightarrow$ `overdue` (vermelho).
  - Caso contrário $\rightarrow$ `due` (amarelo).
- **Prevenção de Sobreposição de Contratos (`Overlaps`):**
  - Dois contratos no mesmo imóvel não podem ter períodos vigentes sobrepostos, sejam residenciais ou de temporada.
- **Validação Estrita de Documentos:**
  - Verificação de CPF com cálculo do dígito verificador.
  - Validação de formato de e-mail compartilhado entre frontend e backend via regex.

---

## 5. Motor de Persistência Dual-Dialeto (SQLite & PostgreSQL)

O Aluxen implementa suporte transparente a **dois motores de banco de dados**, compartilhando o mesmo código de aplicação.

```
       +-------------------------------------------------------------+
       |               domain.Repositories (Interfaces)              |
       +------------------------------+------------------------------+
                                      |
              +-----------------------+-----------------------+
              |                                               |
              v                                               v
+-----------------------------+               +-------------------------------+
|  sqlite.Store (sqlcgen)     |               |   postgres.Store (sqlcgen)    |
|  driver: modernc.org/sqlite |               |   driver: jackc/pgx/v5 (std)  |
|  modo: WAL, foreign keys=ON |               |   pool nativo database/sql    |
+-----------------------------+               +-------------------------------+
```

### Resolução de Conexão no Boot
A variável `DATABASE_URL` dita o motor ativado:
- Se iniciar com `postgres://` ou `postgresql://`, a aplicação inicializa o pool PostgreSQL via `jackc/pgx/v5/stdlib`.
- Caso contrário, ou com prefixo `file:`, inicializa o SQLite em modo WAL (`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;`).

### Paridade Rígida de Esquema (`internal/infrastructure/db/parity`)
O projeto mantém dois arquivos DDL canônicos: `schema/sqlite.sql` e `schema/postgres.sql`.
- Um teste de integração automatizado (`parity_test.go`) inspeciona os catálogos dos dois bancos em tempo de teste.
- O teste valida paridade absoluta de: **nomes de tabelas**, **nomes de colunas**, **obrigatoriedade (`NOT NULL`)**, **chaves primárias**, **chaves estrangeiras e cláusulas ON DELETE**, e **índices**.
- Divergências quebram o CI e impedem o merge.

---

## 6. Locação de Temporada & Comparador de Vistorias por Visão (Tier 2)

O módulo de temporada atende imóveis de aluguel por diária e curta permanência, introduzindo gestão de caução e vistorias fotográficas com conferência automatizada por visão computacional.

### Fluxo da Comparação de Vistorias

```
[Locatário Entrada] ---> Fotos de Entrada (10 fotos, máx 8MB) ---> Armazenamento Local
                                                                        |
[Locatário Saída]   ---> Fotos de Saída   (10 fotos, máx 8MB) ---> Armazenamento Local
                                                                        |
                                                                        v
                                                          [POST /api/inspections/compare]
                                                                        |
                                                                        v
                                                    +---------------------------------------+
                                                    |  ai.PhotoComparator (Infraestrutura)  |
                                                    |  Monta payload multimodal com fotos   |
                                                    |  Instrução com vocabulário fechado    |
                                                    +-------------------+-------------------+
                                                                        |
                                                                        v
                                                    +---------------------------------------+
                                                    |   Resposta do Provedor (JSON Tipado)  |
                                                    |   { findings, confidence, summary }   |
                                                    +-------------------+-------------------+
                                                                        |
                                                                        v
                          +-----------------------------------------------------------------------------------+
                          | Camada de Aplicação (application.CompareLeaseInspections):                         |
                          | 1. Valida vocabulário de severidade (ok | atencao | dano). Fora disso -> 502.     |
                          | 2. Confere se fotos citadas existem de fato na vistoria enviada.                  |
                          | 3. Achado citando foto desconhecida ou sem foto -> DESCARTADO e contado no resumo.|
                          | 4. Gera código de autenticidade AUTH-XXXX e prepara laudo técnico.                |
                          +-----------------------------------------------------------------------------------+
                                                                        |
                                                                        v
                                                          [GET /comparison-pdf?lease_id=]
                                                          Laudo A4 vetorial assinado com selo
```

### Racional Jurídico & Técnico: "Subsídio vs Dano Presumido"
- O laudo emitido pelo sistema traz expressamente a advertência legal: **o documento constitui subsídio técnico e informativo para conferência entre locador e locatário, não configurando presunção jurídica de culpa ou imputação automática de débito.**
- Isso blinda o locador contra contestações judiciais de enriquecimento sem causa ou imputação indevida de avaria.

---

## 7. Inteligência de Documentos: Baixa Automática, Intake & Reajuste

O Aluxen processa comprovantes e contratos através de pipelines determinísticos assistidos por modelos de linguagem e visão.

### 1. Leitura de Comprovantes Pix / TED (`POST /api/payments/proof`)
1. O upload do comprovante grava o arquivo na pasta de armazenamento e registra uma entrada na tabela `payment_proofs` com status `pending`.
2. A imagem/PDF é enviada para o endpoint de visão configurado (`AI_API_URL` / `AI_MODEL`).
3. A IA retorna JSON com: `amount` (centavos), `date`, `payer_name`, `payer_cpf`, `recipient_name`, `identifier` (autenticação Pix) e `confidence` (0.0 a 1.0).
4. **Decisão por Código (`EvaluatePaymentMatch`):**
   - O código busca recibos abertos do locatário.
   - Se o valor bate com precisão de centavos, a data é compatível e a confiança $\ge$ `AI_CONFIDENCE_THRESHOLD` (padrão `0.9`), a quitação é efetivada e o recibo marcado como pago.
   - Se houver divergência de valor ou baixa confiança, o comprovante é remetido para a **Fila de Revisão Humana** (aba *Revisão*).

### 2. Onboarding por Documento (Intake)
- Permite o upload de contratos antigos (PDF), CNH/RG ou comprovantes de endereço.
- O sistema monta uma **proposta de cadastro** na aba *Intake*.
- Nenhum registro definitivo é criado no banco sem a conferência visual e confirmação expressa do locador.

### 3. Reajuste Anual Assistido (IGP-M / IPCA)
- Calcula o reajuste proporcional de acordo com a data base e o índice contratado.
- Exibe prévia do novo valor e emite minuta formal de notificação pronta para envio via WhatsApp ou e-mail.

---

## 8. WhatsApp Bidirecional & Agente de Cobrança / Atendimento

A arquitetura de mensageria substitui o antigo robô de automação de navegador (Puppeteer) por um canal HTTP limpo sobre a API do **WAHA** (WhatsApp HTTP API) ou a **Cloud API oficial da Meta**.

### Modos de Operação
- **Modo Demo (`WHATSAPP_MODE=demo`):** opera via WAHA sobre WhatsApp Web. Indicado para testes e homologação. Permite webhook aberto quando sem segredo configurado.
- **Modo Cliente (`WHATSAPP_MODE=client`):** canal oficial com assinatura criptográfica `X-Webhook-Hmac` obrigatória em todas as mensagens recebidas (*fail-closed*).

### Agente de Atendimento com Escopo Fechado (Tool-Calling)
Ao receber mensagens no webhook (`POST /api/whatsapp/webhook`):
1. A mensagem é persistida e vinculada ao locatário pelo número do telefone.
2. O agente processa a demanda executando apenas **ferramentas de leitura estritamente tipadas** e escopadas ao locatário solicitante:
   - `get_open_receipts`: consulta débitos em aberto.
   - `get_lease_details`: consulta dados do contrato ativo.
   - `create_maintenance_request`: protocola solicitação de manutenção.
3. Se a pergunta envolver interpretação jurídica, renegociação complexa ou solicitação fora do escopo, o sistema executa o **escalonamento para o locador**, notificando-o no painel e mantendo a conversa aberta para intervenção manual.

### Régua de Cobrança Automatizada (`POST /api/collection/run`)
Aplica estratégias graduais baseadas no atraso do título:
- **1 a 6 dias:** Lembrete amigável com código Pix e link de visualização.
- **7 a 14 dias:** Notificação formal de atraso e solicitação de regularização.
- **15+ dias:** Proposta de negociação amigável ou parcelamento pré-aprovado.
- Suporta parâmetro `dry_run=true` para simulação prévia sem envio real de mensagens.

---

## 9. Segurança em Camadas: Sessão, CSRF, Rate Limit & Auditoria

A aplicação implementa defesa em profundidade sem dependência de middlewares de terceiros.

```
Incoming Request
       │
       ▼
[RequestId Middleware]       Atribui identificador único alfanumérico de 8 caracteres
       │
       ▼
[HTTPS Enforcer]             Em produção, exige HTTPS ou redireciona
       │
       ▼
[Rate Limiter]               Janela de 60s por IP e Global; bloqueio progressivo com Retry-After
       │
       ▼
[Session Middleware]         Decodifica e valida assinatura HMAC-SHA256 do cookie
       │
       ▼
[Password Change Gate]       Se senha padrão, bloqueia acesso e redireciona para /settings?forced=1
       │
       ▼
[CSRF Middleware]            Em mutações (POST/PUT/DELETE): valida double-submit token com hmac.Equal
       │
       ▼
[Target Handler]             Executa caso de uso e registra entrada estruturada na Trilha de Auditoria
```

### 1. Cookies de Sessão Assinados
- Nomes: `__aluxen_session` (administrador) e `__aluxen_client_session` (portal do locatário).
- Formato: `<id_sessao_base64>.<assinatura_hmac_sha256>`.
- Atributos: `HttpOnly`, `SameSite=Lax`, `Path=/`, expiração em 30 dias, e `Secure` ativo em produção.

### 2. Dupla Proteção CSRF
- Mutações HTTP exigem envio do token no cookie `csrf_token` e no payload (campo de formulário ou header `X-CSRF-Token`).
- A comparação dos hashes é efetuada em **tempo constante** via `crypto/subtle` para evitar ataques de temporização (*timing attacks*).

### 3. Trilha de Auditoria com Diffs Reversíveis
- A tabela `audit_logs` grava o snapshot das alterações (`before` e `after`).
- Registra endereço IP, ator responsável, entidade manipulada e metadados contextuais.
- Permite exportação completa em formato CSV (`GET /audit/export`) com suporte a caracteres acentuados (UTF-8 com BOM).

---

## 10. Interface com o Usuário, Templates Nativos & PWA Offline

A interface web descarta SPAs pesadas (React/Vue/Angular) em favor de renderização veloz no servidor com aprimoramento progressivo em JavaScript vanilla.

### Renderização e Estáticos Embutidos
- Todos os arquivos da pasta `internal/infrastructure/web/templates` e `internal/infrastructure/web/static` são compilados para dentro do binário Go através da diretiva `//go:embed`.
- Isso elimina a necessidade de pastas de assets externas no container de produção: o executável é completamente autocontido.

### Fila de Sincronização Offline (PWA)
- Suporte a Progressive Web App com manifesto (`manifest.webmanifest`) e Service Worker (`sw.js`).
- Mutações disparadas sem conexão com a internet são enfileiradas no `localStorage` do navegador (`aluxen.offline.queue`).
- Ao restabelecer o sinal de rede, o script `app.js` esvazia a fila transmitindo as operações em lote para o servidor e atualizando a interface em tempo real.
- Uploads pesados (fotos de vistoria e comprovantes) exigem conexão online obrigatória e não entram na fila offline.

---

## 11. Catálogo Completo de Rotas HTTP

| Método | Rota | Autenticação | Função / Descrição |
|---|---|---|---|
| `GET` | `/healthz` | Pública | Probe de liveness/readiness para orquestradores (K8s/Docker) |
| `GET` | `/readyz` | Pública | Confere se as conexões de banco e armazenamento estão ativas |
| `GET` | `/api/health` | Pública | Diagnóstico com dialeto de banco, runtime e tempo de uptime |
| `GET` | `/api/info` | Admin | Informações do sistema, contadores de registros e build |
| `GET` | `/metrics` | Admin | Métricas agregadas da aplicação |
| `GET` | `/` | Pública | Redireciona para `/dashboard` se autenticado, ou `/login` |
| `GET` | `/login` | Pública | Exibe tela de autenticação do administrador |
| `POST` | `/login` | Pública + RateLimit | Processa login administrativo (gera cookie assinado) |
| `GET/POST`| `/logout` | Pública | Destrói a sessão do administrador |
| `GET` | `/dashboard` | Admin | Painel principal (abas: `dashboard`, `tenants`, `properties`, `leases`, `receipts`, `history`, `season`, `calendar`, `conversations`, `review`, `intake`, `adjustment`) |
| `POST` | `/dashboard` | Admin + CSRF | Mutações gerais de cadastro e lançamentos financeiros |
| `POST` | `/dashboard/deposit` | Admin + CSRF | Registro e estorno/devolução de caução de temporada |
| `POST` | `/dashboard/inspection` | Admin + CSRF | Criação e exclusão de auto de vistoria com galeria de fotos |
| `POST` | `/api/inspections/compare` | Admin + CSRF | Compara fotos de entrada e saída via Visão Computacional |
| `GET` | `/settings` | Admin | Página de configurações da instância e do locador |
| `POST` | `/settings` | Admin + CSRF | Salva dados do locador, URL pública ou troca de senha |
| `GET` | `/portal/login` | Pública | Página de autenticação da Área do Cliente |
| `POST` | `/portal/login` | Pública + RateLimit | Login do locatário via CPF e senha |
| `GET` | `/portal` | Locatário | Painel exclusivo do locatário (meus contratos e recibos) |
| `GET/POST`| `/portal/password`| Locatário + CSRF | Troca de senha de acesso do locatário |
| `GET/POST`| `/portal/logout` | Locatário | Encerramento da sessão do locatário |
| `GET` | `/pdf` | Admin/HMAC/Titular | Emissão de recibo de aluguel em PDF vetorial |
| `GET` | `/contract-pdf` | Admin/HMAC/Titular | Emissão de contrato de locação completo em PDF |
| `GET` | `/inspection-pdf` | Admin/HMAC/Titular | Emissão de auto de vistoria com fotos em PDF |
| `GET` | `/deposit-pdf` | Admin/HMAC/Titular | Emissão de recibo de devolução de caução em PDF |
| `GET` | `/comparison-pdf` | Admin/HMAC/Titular | Emissão de laudo técnico de comparação de vistorias em PDF |
| `GET` | `/validar/recibo/{id}` | Pública | Validação pública de autenticidade de recibo via QR |
| `GET` | `/validar/contrato/{id}`| Pública | Validação pública de autenticidade de contrato via QR |
| `GET` | `/validar/vistoria/{id}`| Pública | Validação pública de autenticidade de vistoria via QR |
| `GET` | `/validar/caucao/{id}` | Pública | Validação pública de autenticidade de caução via QR |
| `GET` | `/api/whatsapp/status` | Admin | Estado da conexão com o canal do WhatsApp |
| `POST` | `/api/whatsapp/login/start` | Admin + CSRF | Inicia sessão de pareamento QR Code |
| `GET` | `/api/whatsapp/login/status` | Admin | Consulta status do pareamento e imagem do QR Code |
| `POST` | `/api/whatsapp/login/cancel`| Admin + CSRF | Cancela sessão de pareamento em curso |
| `POST` | `/api/whatsapp/logout` | Admin + CSRF | Desconecta a instância do WhatsApp |
| `POST` | `/api/whatsapp/preview` | Admin + CSRF | Prévia do texto e link `wa.me` de cobrança/documento |
| `POST` | `/api/whatsapp/send` | Admin + CSRF | Enfileira job assíncrono de envio de mensagem |
| `GET` | `/api/whatsapp/send/status`| Admin | Consulta progresso de job de envio |
| `POST` | `/api/whatsapp/webhook` | Assinatura HMAC | Webhook de entrada de mensagens do WAHA/Meta |
| `GET` | `/api/conversations` | Admin | Listagem estruturada das conversas do WhatsApp |
| `POST` | `/dashboard/conversation`| Admin + CSRF | Envia resposta manual ou escala atendimento |
| `POST` | `/api/collection/run` | Admin + CSRF | Dispara régua de cobrança automática dos inadimplentes |
| `POST` | `/api/payments/proof` | Admin/Webhook | Upload e processamento por visão de comprovante Pix/TED |
| `GET` | `/api/review` | Admin | Listagem de pendências da fila de revisão humana |
| `POST` | `/dashboard/review` | Admin + CSRF | Aprova ou recusa com justificativa item em revisão |
| `POST` | `/api/intake/upload` | Admin + CSRF | Upload de documento legado para extração |
| `POST` | `/api/intake/process` | Admin + CSRF | Extração de dados cadastrais por IA |
| `POST` | `/api/intake/confirm` | Admin + CSRF | Efetiva proposta do intake no banco de dados |
| `GET` | `/api/report/monthly` | Admin | Relatório consolidado mensal da carteira imobiliária |
| `POST` | `/api/report/ask` | Admin + CSRF | Consulta analítica em linguagem natural sobre o banco |
| `GET` | `/audit` | Admin | Consulta paginada dos registros da trilha de auditoria |
| `GET` | `/audit/export` | Admin | Exportação da trilha de auditoria em CSV estruturado |
| `GET` | `/export` | Admin | Exportação completa do banco da instância em JSON |
| `POST` | `/import` | Admin + CSRF | Restauração de backup JSON com sobrescrita controlada |

---

## 12. Governança e Verificação Normativa

O desenvolvimento deste port é estritamente auditado e governado pela suite **PWN (`pwn`)**:

- **Obrigatoriedade de Contratos V4:** cada pacote e caso de uso responde a contratos normativos congelados em `.pwn/work/NNNN/CTR-*.json`.
- **Portões Determinísticos (Gates):**
  - `GATE-DISC-REQ`: alinhamento entre descoberta e requisitos.
  - `GATE-REQ-PRD`: completude e rastreabilidade entre requisitos e PRD.
  - `GATE-PRD-SPEC`: congruência das especificações técnicas.
  - `GATE-SPEC-PLAN`: atomicidade e viabilidade do grafo de tarefas.
  - `GATE-PLAN-CONTRACT`: integridade dos contratos e critérios de aceite.
- **Auditoria de Evidências (`task_evidence.ts`):** 93 verificações determinísticas de critérios de aceite auditadas e atestadas em `.todo/evidence/` com 100% de aprovação.
