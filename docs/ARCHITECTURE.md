# Arquitetura do Sistema — Aluxen

Este documento descreve detalhadamente a arquitetura de software, fluxo de dados, modelo de persistência e padrões de design adotados no **Aluxen**.

---

## 1. Visão Geral e Princípios

O Aluxen foi projetado para ser um sistema de gestão imobiliária de alta performance, autocontido e resiliente, operando com consumo mínimo de recursos computacionais.

### Princípios Norteadores:
1. **Zero Sobrecarga de Runtime**: Uso nativo do **Bun** como runtime TypeScript/JavaScript e do driver nativo **`bun:sqlite`**, sem camadas de abstração desnecessárias ou servidores de banco externos obrigatórios.
2. **SSR com Hidratação Reativa (Híbrido SSR + CSR)**: A primeira renderização da página ocorre no servidor via `react-dom/server` (`renderToString`), entregando HTML estático instantâneo com SEO e First Contentful Paint (FCP) imediatos. Em seguida, o bundle de cliente (`public/dist/client.js`) é hidratado no navegador pelo React 19 para permitir reatividade rica (modais, filtros dinâmicos, formulários).
3. **Resiliência e Tolerância a Falhas Offline**: Suporte nativo a Progressive Web App (PWA) e fila de sincronização em `localStorage` para permitir operações essenciais mesmo sem conectividade com a rede.
4. **Segurança Criptográfica em Documentos**: Recibos e contratos gerados contêm chaves de autenticidade HMAC-SHA256 e QR Codes vinculados a um portal público de validação independente.

---

## 2. Diagrama de Arquitetura

```
+-------------------------------------------------------------------------+
|                              Navegador / Cliente                        |
|                                                                         |
|  +------------------------+   +-------------------+   +--------------+  |
|  |  React 19 Hidratado    |   |  Fila Offline     |   |  PWA / Cache |  |
|  | (Dashboard, Settings)  |   |  (localStorage)   |   |  (Manifest)  |  |
|  +-----------+------------+   +---------+---------+   +--------------+  |
+--------------|--------------------------|-------------------------------+
               | HTTP / Fetch             | Re-sync ao voltar online
               v                          v
+-------------------------------------------------------------------------+
|                       Servidor Bun / Hono (Porta 3000)                  |
|                                                                         |
|  +-------------------------------------------------------------------+  |
|  |                          Hono Middleware                          |  |
|  |       Logger, Static Files (/public), Rate Limiter, Cookies       |  |
|  +---------------------------------+---------------------------------+  |
|                                    |                                    |
|          +-------------------------+-------------------------+          |
|          |                                                   |          |
|          v                                                   v          |
|  +---------------+                                   +---------------+  |
|  | Rotas Web     |                                   | Rotas de API  |  |
|  | /login        |                                   | /api/health   |  |
|  | /dashboard    |                                   | /api/whatsapp |  |
|  | /settings     |                                   | /export       |  |
|  | /validar/...  |                                   | /import       |  |
|  +-------+-------+                                   +-------+-------+  |
|          |                                                   |          |
|          +-------------------------+-------------------------+          |
|                                    |                                    |
|                                    v                                    |
|  +-------------------------------------------------------------------+  |
|  |                        Serviços e Utilitários                     |  |
|  |  - Auth & Sessões (bcryptjs, cookie HTTP-only)                    |  |
|  |  - Geração de PDF (pdf-lib, recibos e contratos)                  |  |
|  |  - Tokens Criptográficos & QR Codes (HMAC-SHA256, qrcode)         |  |
|  |  - WhatsApp Service & Browser Headless (Puppeteer + Chromium)     |  |
|  +---------------------------------+---------------------------------+  |
|                                    |                                    |
|                                    v                                    |
|  +-------------------------------------------------------------------+  |
|  |                        Camada de Dados                            |  |
|  |               Drizzle ORM + Driver bun:sqlite                     |  |
|  |            Migrações automáticas em runtime + WAL                 |  |
|  +---------------------------------+---------------------------------+  |
+------------------------------------|------------------------------------+
                                     v
                       +---------------------------+
                       |   SQLite (aluxen.db)      |
                       |   Modo WAL, Foreign Keys  |
                       +---------------------------+
```

---

## 3. Componentes da Stack

### 3.1. Runtime: Bun
- Execução direta de TypeScript sem etapa de transpilação prévia no servidor.
- Alta taxa de requisições por segundo e inicialização em milissegundos.
- Bundler nativo (`Bun.build`) configurado em `src/build-client.ts` para compilar o código do cliente React em um único arquivo minificado (`public/dist/client.js`).

### 3.2. Framework Web: Hono
- Framework ultrarrápido, leve e com tipagem estrita de rotas.
- Roteamento declarativo com suporte a middlewares de logging, cookies assinados e arquivos estáticos.
- Integração limpa com o ciclo de vida do Bun via `export default { fetch: app.fetch, port }`.

### 3.3. Banco de Dados: SQLite & Drizzle ORM
- **Driver**: `bun:sqlite` nativo (acesso direto à C-API do SQLite via memória compartilhada).
- **Pragmas Ativados**:
  - `journal_mode = WAL` (Write-Ahead Logging): leituras e escritas concorrentes sem travamento de arquivo.
  - `foreign_keys = ON`: integridade referencial em nível de banco de dados.
- **Drizzle ORM**: Tipagem segura ponta a ponta sem queries mágicas ou metaprogramação pesada em runtime.
- **Migrações**: Gerenciadas via pasta `drizzle/` com execução automática no boot da aplicação através de `src/db/migrate.ts`.

### 3.4. Frontend: React 19 SSR Híbrido
- **Server Side Rendering (SSR)**: Em `src/index.tsx`, as páginas (`LoginPage`, `DashboardPage`, `SettingsPage`, `VerificationPage`) são renderizadas com `renderToString()`. Os dados iniciais são injetados em uma tag `<script id="__DATA__">` em formato JSON.
- **Client Side Hydration**: O script `src/client/index.tsx` recupera `window.__INITIAL_DATA__` e invoca `hydrateRoot(rootElement, <Component {...props} />)`. Isso ativa todos os listeners de evento, animações e estados sem recarregar a tela.
- **Estilização**: CSS puro sem dependência de frameworks pesados (`public/styles/globals.css`), implementando design glassmorphism moderno, suporte nativo a temas Claro/Escuro (persistido via `data-theme`) e total responsividade para dispositivos móveis.

---

## 4. Modelo de Dados e Relacionamentos

O esquema está declarado em `src/db/schema.ts`:

### 4.1. Tabelas Principais

1. **`users`**:
   - `id`: Identificador único (UUIDv7 gerado via `src/utils/id.ts`).
   - `username`: Nome de usuário único para login.
   - `passwordHash`: Hash de senha computado via `bcryptjs` (custo 10).
   - `createdAt`: Timestamp de criação.

2. **`tenants`** (Inquilinos):
   - `id`: UUIDv7.
   - `name`: Nome completo.
   - `email`: Endereço de email.
   - `phone`: Número de telefone / WhatsApp com DDD.
   - `cpf`: Cadastro de Pessoa Física formatado.
   - `createdAt`, `updatedAt`: Timestamps de auditoria.

3. **`properties`** (Imóveis):
   - `id`: UUIDv7.
   - `name`: Identificador ou nome amigável do imóvel (ex.: "Apto 302", "Casa de Praia").
   - `address`: Logradouro e número.
   - `city`: Cidade.
   - `state`: Unidade Federativa (UF).
   - `zip`: CEP.
   - `createdAt`, `updatedAt`: Timestamps de auditoria.

4. **`leases`** (Contratos de Locação):
   - `id`: UUIDv7.
   - `tenantId`: Referência FK para `tenants.id` com `ON DELETE CASCADE`.
   - `propertyId`: Referência FK para `properties.id` com `ON DELETE CASCADE`.
   - `startDate`: Data de início do contrato (`YYYY-MM-DD`).
   - `endDate`: Data de término do contrato (`YYYY-MM-DD`).
   - `dueDay`: Dia do mês para vencimento do aluguel (1 a 31).
   - `rentAmount`: Valor do aluguel mensal (`REAL`).
   - `status`: `'active'` | `'terminated'`.
   - `authToken`: Token HMAC para validação pública e download do contrato.
   - `createdAt`, `updatedAt`: Timestamps.

5. **`receipts`** (Pagamentos & Recibos):
   - `id`: UUIDv7.
   - `leaseId`: FK opcional para `leases.id` (`ON DELETE CASCADE`).
   - `tenantId`: FK opcional para `tenants.id` (`ON DELETE SET NULL`).
   - `propertyId`: FK opcional para `properties.id` (`ON DELETE SET NULL`).
   - `category`: Categoria (`'rent'` para aluguel contratual ou `'extra'` para taxas/despesas avulsas).
   - `description`: Descrição opcional (obrigatória para cobranças avulsas).
   - `dueDate`: Data de vencimento (`YYYY-MM-DD`).
   - `paidAt`: Timestamp de liquidação (formato ISO ou `YYYY-MM-DDTHH:mm`). Se nulo, o título está em aberto.
   - `amount`: Valor cobrado (`REAL`).
   - `paidAmount`: Valor efetivamente pago (`REAL`). Permite pagamentos parciais.
   - `pdfPath`: Caminho do arquivo em disco (quando persistido).
   - `generatedKey`: Chave única (`lease_id:YYYY-MM`) para prevenir duplicidade de faturamento do mesmo contrato no mesmo mês.
   - `authToken`: Token HMAC para validação pública e download do recibo.
   - `createdAt`, `updatedAt`: Timestamps.

6. **`settings`** (Configurações Chave-Valor):
   - `key`: Identificador único da configuração (`landlord_name`, `landlord_cpf`, `landlord_email`, `app_url`, etc.).
   - `value`: Conteúdo em formato texto ou JSON.

7. **`audit_logs`** (Trilha de Auditoria):
   - `id`: UUIDv7.
   - `created_at`: Timestamp local com milissegundos (`YYYY-MM-DDTHH:mm:ss.SSS`).
   - `actor_id` / `actor_name`: Usuário autenticado que executou a ação (`NULL` em acessos anônimos, como tentativas de login negadas e emissões por link público).
   - `action`: Ação executada (ex.: `tenant.update`, `receipt.pay`, `document.receipt`, `auth.login`).
   - `entity` / `entity_id` / `entity_label`: Área afetada e rótulo humano do registro no momento do evento (nome do inquilino, do imóvel, competência do pagamento) — preservado mesmo após exclusão do registro.
   - `summary`: Descrição legível do evento em português.
   - `changes`: JSON `[{ field, label, from, to }]` com o diff campo a campo **já formatado** (moeda em BRL, datas em `dd/mm/aaaa`, situações traduzidas).
   - `metadata`: JSON com contexto adicional (competência gerada, tamanho do lote, protocolo de envio, motivo de falha).
   - `origin`: `web` (painel autenticado), `public` (link com token) ou `job` (rotina automática).
   - `ip`, `user_agent`, `request_id`: Correlação com o log estruturado da requisição.
   - Índices em `created_at`, `(entity, entity_id)` e `action`.

   A tabela é **append-only na prática**: nenhuma rota atualiza ou remove eventos, e segredos (senhas, tokens, cookies) nunca são gravados.

---

## 5. Fluxo de Execução de Requisições

### 5.1. Fluxo de Autenticação
1. O usuário submete o formulário `/login` com `username` e `password`.
2. O servidor valida o rate limit de IP (máximo de tentativas por minuto).
3. `loginUser` busca o usuário no SQLite e verifica a senha com `bcrypt.compare`.
4. Em caso de sucesso, gera um cookie HTTP-only `__aluxen_session` criptografado contendo o ID do usuário.
5. Em caso de falha, retorna status HTTP 401 e mensagem amigável no card de login.

### 5.2. Fluxo de Mutations no Dashboard (`POST /dashboard`)
Para preservar a compatibilidade de formulários HTML padrão e permitir envio offline e via AJAX, todas as mutações utilizam um campo `intent`:

| Intent | Descrição | Regras de Negócio |
|---|---|---|
| `tenant.create` / `tenant.update` | Cria ou atualiza inquilino | Valida campos obrigatórios (nome, email, CPF). |
| `tenant.delete` | Remove inquilino | Bloqueia se o inquilino possuir contratos ativos. |
| `property.create` / `property.update` | Cria ou atualiza imóvel | Valida endereço e dados geográficos. |
| `property.delete` | Remove imóvel | Bloqueia se o imóvel possuir contratos ativos. |
| `lease.create` / `lease.update` | Cria ou atualiza contrato | Valida intervalo de datas e dia de vencimento. |
| `lease.terminate` / `lease.reactivate` | Altera status do contrato | Muda status para `'terminated'` ou `'active'`. |
| `receipt.create` / `receipt.update` | Cria ou atualiza recibo | Suporta categoria de aluguel ou avulsa; calcula baixa se houver data de pagamento. |
| `receipt.generate-month` | Geração mensal em lote | Cria recibos para todos os contratos ativos do mês selecionado que ainda não foram gerados. |
| `receipt.batch-pay` | Baixa em lote | Marca múltiplos recibos como pagos no timestamp atual. |
| `receipt.delete` | Remove recibo | Exclui o registro e arquivos associados. |

---

### 5.3. Fluxo de Auditoria

Toda mutação bem-sucedida em `POST /dashboard`, `POST /settings`, emissões de PDF, envios por WhatsApp, backups e acessos passa por `recordAudit()` (`src/utils/audit.ts`):

1. O contexto da requisição é montado uma única vez por request (`createAuditContext`): usuário, IP (respeitando `x-forwarded-for`), user-agent, `requestId` e origem.
2. Para cada operação, o estado **antes** é lido do banco, a mutação é aplicada e o estado **depois** é relido.
3. `diffAuditFields()` compara os dois snapshots conforme a especificação de campos da entidade e devolve apenas o que realmente mudou, com os valores formatados para leitura.
4. O evento é inserido em `audit_logs` e replicado no log estruturado (`scope: audit`).

Regras de projeto:

- Operações sem alteração real (salvar um formulário idêntico) **não** geram evento — a trilha registra mudanças, não cliques.
- A gravação é "melhor esforço": se a auditoria falhar, a operação de negócio já efetivada não é desfeita, mas a falha é registrada em `error` no log.
- Operações em lote (baixa/exclusão de vários recibos) geram **um evento por recibo**, com `metadata.batch` indicando o tamanho do lote.

### 5.4. Observabilidade e Logs

`src/utils/logger.ts` implementa o logger estruturado usado por todo o runtime (zero dependências):

- **Níveis**: `debug`, `info`, `warn`, `error` e `silent`, controlados por `LOG_LEVEL`.
- **Formatos**: `pretty` (linha legível, padrão em TTY) e `json` (JSON Lines, padrão fora de TTY e em containers), controlados por `LOG_FORMAT`.
- **Escopos**: `http` (requisições), `audit` (trilha), `db`, `db.migrate`, `db.seed`, `whatsapp`, `whatsapp.login`, `whatsapp.browser` e `app` (boot).
- **Redação**: qualquer campo cujo nome contenha `password`/`senha`, `token`, `secret`, `cookie`, `authorization`, `api_key`, `hash` ou `session` é substituído por `[redigido]` antes da escrita.
- **Erros**: objetos `Error` são serializados com `name`, `message` e as primeiras linhas do `stack`.
- **Middleware HTTP**: gera um `requestId` curto por requisição, mede a duração e classifica o evento — estáticos em `debug`, 4xx em `warn`, 5xx em `error` e requisições acima de 1 s em `warn`. O mesmo `requestId` aparece na página/JSON de erro e na trilha de auditoria, permitindo reconstruir o que aconteceu a partir de um protocolo.
- **Erros não tratados**: `app.onError` registra o stack trace e responde com uma página amigável (ou JSON, para rotas `/api`), em vez do erro genérico do framework.

---

## 6. Geração Dinâmica de Documentos (PDF)

A geração de documentos é processada em memória com a biblioteca `pdf-lib`:
- **`src/utils/pdf.ts`**: Cria o Recibo de Pagamento de Aluguel formatado em folha A4 com identidade visual do Aluxen, incluindo:
  - Cabeçalho institucional.
  - Dados do locatário e do imóvel.
  - Quadro resumo financeiro (valor, vencimento, data de liquidação).
  - Declaração de quitação com valores monetários por extenso em português.
  - Assinatura digital ou campo em branco.
  - **QR Code de Validação** e **Código de Autenticidade** no rodapé.
- **`src/utils/contract-pdf.ts`**: Cria o Contrato de Locação Residencial completo em múltiplas páginas com 25 cláusulas jurídicas padrão, campos para assinatura das partes, testemunhas e QR Code de validação.
