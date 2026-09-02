# Aluxen (Bun + SQLite + Hono + Drizzle ORM)

Sistema de gestão de locações imobiliárias reimplementado com foco em alta performance e simplicidade.

## Stack

- **Runtime:** [Bun](https://bun.sh/)
- **Framework Web:** [Hono](https://hono.dev/)
- **Templates:** Hono JSX nativo (`jsxImportSource: "hono/jsx"`)
- **Banco de Dados:** SQLite nativo via `bun:sqlite`
- **ORM / Migrations:** [Drizzle ORM](https://orm.drizzle.team/) + [Drizzle Kit](https://orm.drizzle.team/kit-docs/overview)

## Pré-requisitos

- Bun (>= 1.0)

## Como Rodar

### Instalar dependências
```bash
bun install
```

### Executar em desenvolvimento (com reload automático)
```bash
bun run dev
```

O servidor iniciará em `http://localhost:3000`. As migrações do banco SQLite são executadas automaticamente ao iniciar.

### Executar em produção
```bash
bun run start
```

### Comandos de Banco de Dados (Drizzle)

```bash
# Gerar novas migrações SQL a partir do schema
bun run db:generate

# Executar migrações pendentes
bun run db:migrate

# Sincronizar schema diretamente no banco
bun run db:push

# Abrir o Drizzle Studio para visualizar dados
bun run db:studio
```

### Testes & Tipagem

```bash
# Executar testes unitários e de integração
bun test

# Verificação de tipos TypeScript
bun run typecheck
```

## Estrutura do Projeto

```
.
├── drizzle/              # Migrações SQL geradas pelo Drizzle Kit
├── drizzle.config.ts     # Configuração do Drizzle Kit
├── src/
│   ├── db/
│   │   ├── index.ts      # Conexão SQLite via bun:sqlite e Drizzle client
│   │   ├── schema.ts     # Schemas das tabelas (users, tenants, properties, leases, receipts)
│   │   └── migrate.ts    # Runner de migrações automáticas
│   ├── views/            # Templates Hono JSX
│   │   ├── layout.tsx    # Layout HTML base com CSS responsivo
│   │   ├── home.tsx      # Dashboard geral com métricas
│   │   ├── tenants.tsx   # Gestão de Inquilinos
│   │   ├── properties.tsx# Gestão de Imóveis
│   │   ├── leases.tsx    # Gestão de Locações
│   │   └── receipts.tsx  # Histórico e emissão de Recibos
│   └── index.tsx         # Servidor Hono, rotas e handlers
├── test/
│   └── app.test.ts       # Testes de integração com bun:test
├── tsconfig.json         # Configuração TypeScript com suporte a Hono JSX
└── package.json
```
