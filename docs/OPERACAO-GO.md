# 🚀 Operação do Aluxen (Go) — Manual de Operação e Runbook

> **Guia definitivo do operador para o Aluxen no modelo de uma instância por cliente.**
> Cobre: ambientes de execução (`make dev`, `make test`, `make prod`), provisionamento no Easypanel, ciclo de vida de demos, migração SQLite $\rightarrow$ PostgreSQL, canário, backup por volume, troubleshooting e recuperação de desastres.

---

## 📑 Sumário

0. [O Modelo Operacional](#0-o-modelo-operacional)
1. [Ambientes de Execução Padronizados (Makefile)](#1-ambientes-de-execução-padronizados-makefile)
2. [Provisionamento de Instância no Easypanel](#2-provisionamento-de-instância-no-easypanel)
3. [Ciclo de Vida de Demonstrações (Demos)](#3-ciclo-de-vida-de-demonstrações-demos)
4. [Migração de Dialeto: SQLite $\rightarrow$ PostgreSQL (`cmd/migrate`)](#4-migração-de-dialeto-sqlite--postgresql-cmdmigrate)
5. [Atualização Canário sem Indisponibilidade](#5-atualização-canário-sem-indisponibilidade)
6. [Estratégia de Backup, Restauração & Volumes](#6-estratégia-de-backup-restauração--volumes)
7. [Variáveis de Ambiente e Perfis de Instância](#7-variáveis-de-ambiente-e-perfis-de-instância)
8. [Monitoramento, Probes HTTP & Métricas](#8-monitoramento-probes-http--métricas)
9. [Playbook de Troubleshooting & Recuperação de Desastres](#9-playbook-de-troubleshooting--recuperação-de-desastres)

---

## 0. O Modelo Operacional

Ao contrário de SaaS multi-tenant que misturam dados de centenas de clientes no mesmo banco e processo, o Aluxen opera sob **isolamento físico completo por cliente**:

```
[Internet] ---> Traefik / Caddy (Easypanel Reverse Proxy + TLS Automático)
                       │
       ┌───────────────┼───────────────┐
       ▼               ▼               ▼
[Instância A]   [Instância B]   [Instância Demo]
Container Go    Container Go    Container Go
Volume /data    Volume /data    Volume /data (efêmero)
SQLite/Postgres SQLite/Postgres SQLite (demo seed)
```

| Dimensão | Vantagem Operacional |
|---|---|
| **Isolamento** | Falha de processo, pane de banco ou pico de carga em um cliente não afeta nenhum outro. |
| **Segurança & LGPD** | Dados de clientes distintos nunca compartilham memória ou buffers de banco. |
| **Backup e Exclusão** | Apagar ou migrar um cliente resume-se a copiar ou destruir o volume `/data`. |
| **Atualização** | Pode ser feita gradualmente (canário em 1 cliente de teste, depois os demais). |
| **Flexibilidade de Custo** | Clientes pequenos rodam em SQLite sobre volume local; clientes com grande volume conectam ao PostgreSQL. |

---

## 1. Ambientes de Execução Padronizados (Makefile)

O projeto disponibiliza 3 ambientes de execução nativos orquestrados via `Makefile`:

### 1.1. Desenvolvimento Local (`make dev`)
Desenvolvimento ágil com **live-reload nativo** via `Air` (sem containers, sem lentidão):

```bash
make dev
```
- **O que faz:** compila `./cmd/app` para `./tmp/main` e inicia o servidor escutando em `http://localhost:3000`.
- **Hot-Reload:** monitora alterações em arquivos `.go`, `.html`, `.css`, `.js` e `.sql`.
- **Isolamento:** cria automaticamente `./tmp/dev-data/` para manter o banco SQLite local e uploads isolados da árvore de código.

### 1.2. Ambiente de Testes Automatizados (`make test`)
Execução da suíte completa de testes com infraestrutura provisionada **exclusivamente via Podman**:

```bash
make test
```
- **O que faz:**
  1. Verifica a presença do `podman` no sistema (recusa explicitamente o Docker para garantir fidelidade ao runtime Podman).
  2. Sobe um container isolado com `PostgreSQL 17` (`aluxen-test-postgres`) expondo a porta 5432.
  3. Aguarda o PostgreSQL aceitar conexões via probe `pg_isready`.
  4. Executa `go test ./... -race -coverpkg=./... -coverprofile=coverage.out` exercitando os dialetos SQLite e PostgreSQL.
  5. Valida o **gate de cobertura mínima de 90%**.
  6. Finaliza e remove o container via `trap` no encerramento (tanto em sucesso quanto em falha).

### 1.3. Ambiente de Produção (`make prod`)
Gera a imagem de produção multi-stage estática e sobe a stack via Compose:

```bash
# Auto-detecta Docker ou Podman:
make prod

# Força motor Docker & Docker Compose:
make prod ENGINE=docker

# Força motor Podman & Podman Compose:
make prod ENGINE=podman

# Encerra o ambiente:
make prod-down

# Acompanha logs estruturados em tempo real:
make prod-logs
```

---

## 2. Provisionamento de Instância no Easypanel

A automação de provisionamento vive em `cmd/provision/main.go` e dialoga diretamente com a API do Easypanel.

### Pré-requisitos
No ambiente do operador (sua máquina ou orquestrador):
```env
EASYPANEL_URL="https://painel.seu-dominio.com"
EASYPANEL_API_KEY="seu_token_de_acesso"
EASYPANEL_PROJECT="aluxen"
```

### Criar uma Instância para Cliente Real
```bash
go run ./cmd/provision create \
  --name aluxen-imobiliaria-central \
  --domain imobiliaria.aluxen.com.br \
  --admin-user passoz \
  --admin-pass "SenhaForteAleatoria123!" \
  --image ghcr.io/passoz/aluxen:go
```

O comando realiza de forma idempotente:
1. Cria o serviço no Easypanel com reinício automático (`unless-stopped`).
2. Configura a imagem `ghcr.io/passoz/aluxen:go` (multi-arch AMD64/ARM64).
3. Cria e anexa o volume persistente `/data`.
4. Gera um `SESSION_SECRET` seguro com 64 caracteres hexadecimais.
5. Injeta as variáveis de ambiente necessárias.
6. Registra o domínio com certificado TLS/HTTPS automático via Let's Encrypt.
7. Dispara o primeiro deploy e aguarda a inicialização.

---

## 3. Ciclo de Vida de Demonstrações (Demos)

Instâncias de demonstração são geradas para prospectos com dados fictícios e **data de expiração programada**:

### Criar uma Instância de Demonstração
```bash
go run ./cmd/provision create \
  --name aluxen-demo-mariana \
  --domain demo-mariana.aluxen.com.br \
  --demo \
  --days 3 \
  --image ghcr.io/passoz/aluxen:go
```

Ao passar a flag `--demo`:
- `SEED_DEMO=true` é injetado: o primeiro boot popula automaticamente 5 imóveis e contratos fictícios.
- A etiqueta `expires_at` é gravada nos metadados do serviço com a data limite em UTC.

### Listar e Expirar Demos
```bash
# Listar todas as instâncias e identificar as expiradas:
go run ./cmd/provision list

# Expirar e destruir instâncias que ultrapassaram o prazo:
go run ./cmd/provision expire
```

### Cron Sugerido no Servidor
Para garantir que instâncias de demonstração não consumam recursos indefinidamente, agende no cron do host:
```crontab
0 3 * * * EASYPANEL_URL=... EASYPANEL_API_KEY=... /usr/local/bin/provision expire >> /var/log/aluxen-expire.log 2>&1
```

---

## 4. Migração de Dialeto: SQLite $\rightarrow$ PostgreSQL (`cmd/migrate`)

Quando uma instância cresce ou necessita de replicação externa, o banco SQLite local pode ser migrado para um banco PostgreSQL remoto sem perda de integridade.

### Pré-requisitos
1. Um banco PostgreSQL criado e acessível pela instância.
2. A aplicação parada momentaneamente durante a transferência para evitar escritas concorrentes.

### Executar a Migração
```bash
# 1. Modo Dry-Run (apenas simula e valida compatibilidade de esquemas):
go run ./cmd/migrate \
  --sqlite-path /caminho/do/volume/aluxen.db \
  --postgres-url "postgres://user:pass@host:5432/aluxen?sslmode=require" \
  --dry-run

# 2. Execução Real com Cópia e Verificação de Checksums:
go run ./cmd/migrate \
  --sqlite-path /caminho/do/volume/aluxen.db \
  --postgres-url "postgres://user:pass@host:5432/aluxen?sslmode=require"
```

### O que o Migrador Faz
1. Conecta no SQLite de origem e no PostgreSQL de destino.
2. Aplica automaticamente as migrações Goose no PostgreSQL para garantir que o esquema está idêntico.
3. Inicia uma transação única no PostgreSQL.
4. Transfere os dados tabela por tabela na ordem estrita de dependência de chaves estrangeiras:
   - `users` $\rightarrow$ `tenants` $\rightarrow$ `properties` $\rightarrow$ `leases` $\rightarrow$ `receipts` $\rightarrow$ `deposits` $\rightarrow$ `inspections` $\rightarrow$ `audit_logs` $\rightarrow$ `jobs`.
5. Valida a contagem exata de linhas entre origem e destino.
6. Atualiza as sequências (`sequences`) do PostgreSQL para evitar colisões em novos inserts.
7. Confirma a transação (`COMMIT`).

### Cutover Operacional
1. No painel, altere a variável da instância:
   ```env
   DATABASE_URL=postgres://user:pass@host:5432/aluxen?sslmode=require
   ```
2. Reinicie o container.
3. **Não apague o arquivo `aluxen.db` do volume**: ele permanece como backup frio para eventual rollback imediato.

---

## 5. Atualização Canário sem Indisponibilidade

O modelo de instâncias isoladas permite aplicar novas versões primeiro em uma instância canário (cliente voluntário ou instância interna) antes de atualizar o restante da frota:

```bash
# 1. Atualizar primeiro o cliente canário:
go run ./cmd/provision create \
  --name aluxen-canario \
  --image ghcr.io/passoz/aluxen:sha-novo123

# 2. Validar integridade via probe HTTP:
curl -fsS https://canario.aluxen.com.br/healthz
curl -fsS https://canario.aluxen.com.br/readyz

# 3. Validada a versão, atualizar as demais instâncias:
for cliente in cliente1 cliente2 cliente3; do
  go run ./cmd/provision create --name aluxen-$cliente --image ghcr.io/passoz/aluxen:sha-novo123
done
```

---

## 6. Estratégia de Backup, Restauração & Volumes

### 6.1. Backup de Instâncias SQLite (Padrão)
O banco SQLite opera em modo **WAL** (`Write-Ahead Logging`). O backup consistente pode ser feito a quente com a ferramenta oficial `sqlite3`:

```bash
# Backup consistente do banco a quente (sem parar a aplicação):
sqlite3 /data/aluxen.db ".backup '/backup/aluxen-$(date +%F).db'"

# Backup dos uploads (fotos de vistoria e comprovantes):
rsync -a /data/storage/ /backup/storage-$(date +%F)/
```

### 6.2. Restauração SQLite
Para restaurar:
1. Pare o container da instância.
2. Substitua o arquivo `/data/aluxen.db` pelo arquivo do backup.
3. Remova quaisquer arquivos temporários residuais (`/data/aluxen.db-wal` e `/data/aluxen.db-shm`).
4. Inicie o container.

### 6.3. Backup e Restauração PostgreSQL
```bash
# Backup:
pg_dump "$DATABASE_URL" -Fc -f "/backup/aluxen-pg-$(date +%F).dump"

# Restauração:
pg_restore -d "$DATABASE_URL" --clean --if-exists "/backup/aluxen-pg-2026-09-21.dump"
```

### 6.4. Backup Lógico pelo Produto (`GET /export` & `POST /import`)
O painel administrativo oferece exportação completa de dados em JSON estruturado:
- Acessível na aba de Configurações do painel.
- Permite transferir cadastros e contratos entre instâncias independentemente do dialeto de banco.

---

## 7. Variáveis de Ambiente e Perfis de Instância

Toda a configuração é estritamente 12-Factor, lida do ambiente:

### Matriz de Diferença: Instância de Produção vs Demo

| Parâmetro | Produção (Cliente) | Demonstração (Demo) |
|---|---|---|
| `APP_ENV` | `production` | `production` |
| `SEED_DEMO` | `false` | `true` (popula dados fake no boot) |
| `WHATSAPP_MODE` | `client` (Cloud API oficial) | `demo` (WAHA Webhook) |
| `COOKIE_SECURE` | `true` | `true` |
| `LOG_FORMAT` | `json` | `json` |
| `Etiqueta expires_at` | Nenhuma (perene) | Gravada nos metadados |

---

## 8. Monitoramento, Probes HTTP & Métricas

A imagem distroless do Aluxen conta com ferramentas nativas de verificação de integridade sem depender de utilitários como `curl` ou `wget` no container:

### Probes HTTP da Aplicação
- **`GET /healthz` (Liveness):**
  - Retorna `200 OK` com texto `ok`.
  - Confere se o processo Go e o loop de eventos HTTP estão respondendo. Não onera o banco de dados.
- **`GET /readyz` (Readiness):**
  - Retorna `200 OK` com texto `ready`.
  - Executa um `SELECT 1` no banco de dados e valida o diretório de armazenamento. Retorna `503 Service Unavailable` se o banco estiver inacessível.
- **`GET /api/health`:**
  - Devolve JSON estruturado com dialeto ativo, versão do framework, runtime e carimbo de tempo.

### Healthcheck Nativo no Container
O binário suporta a flag interna `--healthcheck`:
```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/aluxen", "--healthcheck"]
```
Quando invocado com `--healthcheck`, o próprio executável faz uma requisição HTTP interna para `http://127.0.0.1:PORT/healthz` e sai com código `0` ou `1`.

---

## 9. Playbook de Troubleshooting & Recuperação de Desastres

### 9.1. Container Recusa o Boot: "SESSION_SECRET deve ter ao menos 16 caracteres"
- **Causa:** `APP_ENV=production` foi ativado sem definir `SESSION_SECRET` ou com string inferior a 16 caracteres.
- **Solução:** Gere uma chave aleatória segura e injete no ambiente:
  ```bash
  openssl rand -hex 32
  ```

### 9.2. Administrador Bloqueado por Tentativas Excessivas (Rate Limit)
- **Causa:** O usuário errou a senha repetidamente e atingiu o limite de bloqueio progressivo (`LOGIN_LOCK_THRESHOLD`).
- **Solução:**
  - O bloqueio expira automaticamente após o backoff indicado no header `Retry-After`.
  - Para liberação imediata sem reiniciar o container, acesse o banco da instância e zere a contagem de tentativas da tabela de configurações ou reinicie o processo para descarregar o cache em memória do rate limiter.

### 9.3. Reset Emergencial de Senha do Administrador
Caso o locador tenha esquecido a senha mestra da instância:
1. Gere um hash bcrypt de uma nova senha (ex: `NovaSenha123`):
   ```bash
   htpasswd -bnBC 10 "" "NovaSenha123" | tr -d ':\n'
   ```
2. Atualize o registro no SQLite da instância:
   ```bash
   sqlite3 /data/aluxen.db "UPDATE users SET password_hash='<hash_gerado>', must_change_password=1 WHERE username='passoz';"
   ```
3. O administrador poderá efetuar login imediatamente com a nova senha e será compelido a redefini-la no primeiro acesso.

### 9.4. Erro de Banco Travado no SQLite (`database is locked`)
- **Causa:** Algum processo externo montou o volume SQLite sem suporte adequado a travas POSIX ou manteve uma transação de escrita aberta.
- **Solução:**
  1. Certifique-se de que nenhum outro container ou script está montando o mesmo arquivo simultaneamente.
  2. O Aluxen configura automaticamente `PRAGMA busy_timeout = 5000;`.
  3. Verifique se o sistema de arquivos onde o volume reside suporta bloqueios de arquivo (evite montagens NFS v3 sem lock daemons; prefira volumes locais ext4/XFS).
