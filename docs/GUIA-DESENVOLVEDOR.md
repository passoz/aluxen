# 💻 Guia do Desenvolvedor — Aluxen (Go)

> **Manual prático de onboarding, fluxo de trabalho e padrões de engenharia para desenvolvedores no Aluxen.**
> Cobre: setup local, live-reload com Air, ciclo de banco (Goose + sqlc), paridade de esquemas, criação de casos de uso na Clean Architecture, testes automatizados e governança com PWN.

---

## 📑 Sumário

1. [Pré-requisitos & Ferramentas](#1-pré-requisitos--ferramentas)
2. [Ambiente de Desenvolvimento (`make dev`)](#2-ambiente-de-desenvolvimento-make-dev)
3. [Estrutura de Camadas e Regras de Dependência](#3-estrutura-de-camadas-e-regras-de-dependência)
4. [Ciclo de Banco de Dados (sqlc + Goose + Paridade)](#4-ciclo-de-banco-de-dados-sqlc--goose--paridade)
5. [Criando uma Nova Funcionalidade (Passo a Passo)](#5-criando-uma-nova-funcionalidade-passo-a-passo)
6. [Testes Automatizados & Gate de Cobertura (`make test`)](#6-testes-automatizados--gate-de-cobertura-make-test)
7. [Padrões de Código & Boas Práticas](#7-padrões-de-código--boas-práticas)
8. [Governança com PWN (`pwn`)](#8-governança-com-pwn-pwn)

---

## 1. Pré-requisitos & Ferramentas

Para desenvolver no Aluxen, instale as seguintes ferramentas na sua estação de trabalho:

- **[Go 1.27+](https://go.dev/dl/):** Compilador e toolchain oficial.
- **[Air](https://github.com/air-verse/air):** Utilitário de live-reload (`go install github.com/air-verse/air@latest`).
- **[Podman](https://podman.io/):** Motor de containers para execução da suíte completa de testes.
- **[sqlc v1.31+](https://sqlc.dev/):** Compilador de SQL para Go tipado (`go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`).
- **[Make](https://www.gnu.org/software/make/):** Utilitário de automação de tarefas.

---

## 2. Ambiente de Desenvolvimento (`make dev`)

O fluxo local opera sem containers, garantindo feedback imediato e compilação em sub-segundos:

```bash
make dev
```

### O que o comando faz
1. Compila o binário em `./tmp/main` usando o arquivo de configuração `.air.toml`.
2. Inicia o servidor HTTP escutando em `http://localhost:3000`.
3. Assiste em tempo real alterações nos diretórios `cmd/` e `internal/` para arquivos `.go`, `.html`, `.css`, `.js` e `.sql`.
4. Isola o banco SQLite e uploads de teste na pasta descartável `./tmp/dev-data/` (ignorada no git).

---

## 3. Estrutura de Camadas e Regras de Dependência

O projeto segue à risca a **Clean Architecture**:

```
[cmd/app] (Composition Root: instancia dependências reais e inicia o servidor)
   │
   ├──▶ [internal/infrastructure] (Adapters: DB, Web, PDF, AI, WhatsApp, Config)
   │         │
   │         ├──▶ [internal/application] (Use Cases / Interactors de negócio)
   │         │         │
   │         │         └──▶ [internal/domain] (Entidades, Interfaces, Regras Puras)
   │         │
   │         └────────────▶ [internal/domain]
```

### Regras Mandatórias
- **`domain` nunca importa ninguém:** Não conhece `application`, não conhece `infrastructure`, não conhece `sqlc`, não conhece HTTP.
- **Zero SQL em código Go:** Toda instrução SQL reside exclusivamente em `queries/queries.sql` ou nas migrações em `migrations/`. O acesso a dados é sempre via métodos gerados pelo `sqlc`.
- **Handlers HTTP são adaptadores finos:** Handlers em `internal/infrastructure/web` decodificam os dados da requisição, validam a entrada com `validator/v10`, invocam o caso de uso em `application` e codificam a resposta em JSON ou HTML. Nenhuma regra de negócio deve residir em handler.

---

## 4. Ciclo de Banco de Dados (sqlc + Goose + Paridade)

O Aluxen promete e entrega suporte transparente a **SQLite** e **PostgreSQL**. A consistência é garantida pelo fluxo de sincronização:

```
                  1. Altere os DDLs canônicos
                ┌────────────────────────────┐
                │    schema/sqlite.sql       │
                │    schema/postgres.sql     │ (Paridade estrita obrigatória)
                └──────────────┬─────────────┘
                               │
            2. Crie as migrações Goose incrementais
                ┌──────────────┴─────────────┐
                │ migrations/sqlite/NNNN_*.sql   │
                │ migrations/postgres/NNNN_*.sql │
                └──────────────┬─────────────┘
                               │
            3. Adicione as queries SQL tipadas
                ┌──────────────┴─────────────┐
                │     queries/queries.sql    │ (ASCII puro)
                └──────────────┬─────────────┘
                               │
            4. Regenere o código Go fortemente tipado
                ┌──────────────┴─────────────┐
                │ sqlc generate -f sqlc.sqlite.yaml    │
                │ sqlc generate -f sqlc.postgres.yaml  │
                └──────────────┬─────────────┘
                               │
            5. Valide a paridade entre os dois bancos
                ┌──────────────┴─────────────┐
                │ go test ./internal/infrastructure/db/parity │
                └────────────────────────────┘
```

### Regras Cruciais de Banco
- `queries/queries.sql` precisa ser **ASCII puro**: caracteres multi-byte quebram o analisador léxico do `sqlc` para o dialeto SQLite.
- O teste `internal/infrastructure/db/parity/parity_test.go` é obrigatório no CI: se uma coluna for adicionada no SQLite e esquecida no PostgreSQL, o teste quebra.

---

## 5. Criando uma Nova Funcionalidade (Passo a Passo)

Para adicionar uma nova funcionalidade ao Aluxen, siga a sequência de dentro para fora:

### Passo 1: Domínio (`internal/domain/`)
1. Adicione ou estenda as entidades em `entities.go`.
2. Declare regras puras de validação ou cálculo em `rules.go`.
3. Adicione a assinatura do repositório em `ports.go` (ex.: `CreateInvoice(ctx, *Invoice) error`).

### Passo 2: Persistência (`schema/`, `queries/`, `internal/infrastructure/db/`)
1. Atualize `schema/sqlite.sql` e `schema/postgres.sql`.
2. Adicione os arquivos de migração correspondentes em `migrations/sqlite/` e `migrations/postgres/`.
3. Escreva as consultas tipadas em `queries/queries.sql`.
4. Execute `sqlc generate -f sqlc.sqlite.yaml` e `sqlc generate -f sqlc.postgres.yaml`.
5. Implemente os métodos nos adaptadores `internal/infrastructure/db/sqlite/` e `internal/infrastructure/db/postgres/`.
6. Rode `go test ./internal/infrastructure/db/parity` para validar paridade.

### Passo 3: Caso de Uso (`internal/application/`)
1. Crie ou estenda o caso de uso em `internal/application/`.
2. Orquestre as portas recebidas através da struct `Deps`:
   ```go
   func CreateInvoice(ctx context.Context, deps Deps, actx AuditContext, input CreateInvoiceInput) (*domain.Invoice, error) { ... }
   ```
3. Registre a ação correspondente na trilha de auditoria via `deps.Repos.Audit.Insert`.

### Passo 4: Camada Web (`internal/infrastructure/web/`)
1. Crie a função do handler em `handlers_*.go`.
2. Registre a rota no `internal/infrastructure/web/router.go`.
3. Se houver tela, crie ou atualize o template HTML correspondente em `templates/`.

---

## 6. Testes Automatizados & Gate de Cobertura (`make test`)

A qualidade do projeto é resguardada por testes em camadas:

```bash
# Executa a suíte completa com PostgreSQL isolado via Podman e gate >= 90%:
make test
```

### Tipos de Teste no Repositório
- **Testes Unitários:** Testam regras puras de domínio e casos de uso isolados utilizando dublês de repositórios (*fakes* em memória).
- **Testes de Integração de Banco:**
  - Testes com SQLite em memória (`:memory:`).
  - Testes com PostgreSQL real disparados quando a variável `TEST_DATABASE_URL` está preenchida (como no `make test`).
- **Testes de Rota / Handlers:** Exercitam o ciclo HTTP completo com cookies assinados, CSRF e renderização via `httptest.NewServer`.

### Como rodar testes isolados rapidamente durante o desenvolvimento
```bash
# Roda apenas testes de domínio:
go test ./internal/domain/...

# Roda testes de aplicação:
go test ./internal/application/...

# Roda testes de web:
go test ./internal/infrastructure/web/... -race
```

---

## 7. Padrões de Código & Boas Práticas

### Tratamento de Erros
- Use sempre embrulho contextual: `fmt.Errorf("buscar inquilino %s: %w", id, err)`.
- **Nunca retorne `nil, nil`:** Casos de sucesso retornam `(objeto, nil)`; falhas retornam `(nil, err)`.
- Erros de validação com mensagem destinada ao usuário final devem usar `domain.Invalid("Mensagem clara em português.")`.

### Logging Estruturado com `slog`
- Nunca use `fmt.Println` ou `log.Printf`.
- Use o logger injetado com contexto e campos tipados:
  ```go
  h.Logger.InfoContext(ctx, "pagamento registrado com sucesso",
      slog.String("receiptId", receipt.ID),
      slog.Float64("amount", receipt.Amount),
  )
  ```
- **Nunca logue senhas, tokens ou dados pessoais sensíveis não mascarados.**

---

## 8. Governança com PWN (`pwn`)

O desenvolvimento no Aluxen é auditado e governado por contratos normativos:

```bash
# Validar documentos normativos contra JSON Schemas:
pwn validate --work 0001

# Validar o grafo de tarefas e limites de atomicidade:
pwn work plan --work 0001

# Executar todos os gates determinísticos (GATE-DISC-REQ a GATE-PLAN-CONTRACT):
pwn work gate --all --work 0001
```

Mantenha sempre os planos em `.todo/NNNN-tasks.md` sincronizados e com zero *blocking issues*.
