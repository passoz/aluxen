# Catálogo de APIs e Rotas HTTP — Aluxen (Go)

Esta documentação cataloga todos os endpoints HTTP expostos pelo app **Go** do
Aluxen (o app Bun/Hono foi removido do repositório). A fonte da verdade é a
tabela de rotas de `internal/infrastructure/web/router.go` e o comportamento dos
handlers de `internal/infrastructure/web/handlers_*.go`; cada mensagem de erro
citada abaixo é o texto literal devolvido hoje.

---

## Sumário de Rotas

1. [Convenções transversais](#1-convenções-transversais)
2. [Sistema e healthcheck](#2-sistema-e-healthcheck)
3. [Autenticação do administrador](#3-autenticação-do-administrador)
4. [Painel e mutações de negócio](#4-painel-e-mutações-de-negócio)
5. [Configurações da instância](#5-configurações-da-instância)
6. [Métricas, histórico e backup](#6-métricas-histórico-e-backup)
7. [Documentos em PDF e prova pública](#7-documentos-em-pdf-e-prova-pública)
8. [Temporada: caução e vistoria](#8-temporada-caução-e-vistoria)
9. [Comprovantes e onboarding por documento](#9-comprovantes-e-onboarding-por-documento)
10. [Conversas, régua de cobrança e tier 2 assistido](#10-conversas-régua-de-cobrança-e-tier-2-assistido)
11. [WhatsApp (WAHA)](#11-whatsapp-waha)
12. [Área do Cliente (portal do locatário)](#12-área-do-cliente-portal-do-locatário)

---

## 1. Convenções transversais

**Roteamento.** `net/http` com `http.ServeMux` do Go 1.22+: cada rota é
`MÉTODO /path`, com parâmetros de path no padrão `/validar/recibo/{id}`. Rota não
casada cai no handler de 404.

**Formas de autenticação.**

| Forma | Como é transportada | Onde vale |
|---|---|---|
| Sessão do administrador | cookie `__aluxen_session` | painel, configurações, APIs administrativas |
| Sessão do locatário | cookie `__aluxen_client_session` | Área do Cliente e leitura dos próprios documentos |
| Token HMAC do documento | `?token=...` na URL (QR Code / chave AUTH) | PDFs e páginas públicas de validação |
| Pública | sem credencial | `/healthz`, `/readyz`, `/api/health`, `/login`, `/portal/login`, `/validar/*`, estáticos |

Os dois cookies são **assinados**: o valor é `base64url(id|expiração)` seguido de
`"."` e da assinatura HMAC-SHA256 (com `SESSION_SECRET`), comparada em tempo
constante; a expiração é conferida a cada leitura. Flags `HttpOnly`,
`SameSite=Lax`, `Path=/`, validade de 30 dias e `Secure` quando
`COOKIE_SECURE=true` ou `APP_ENV=production`. O cookie não é criptografado — o id
viaja legível, mas um valor forjado não passa na verificação.

**CSRF.** Toda requisição mutante (`POST`, `PUT`, `PATCH`, `DELETE`) exige o
double-submit: cookie `csrf_token` **e** campo/parâmetro `csrf_token` com o mesmo
valor assinado. Sem cookie ou token, a resposta é `403` em texto simples com
`csrf: token ausente`; token divergente ou com assinatura inválida devolve `403`
com `csrf: token inválido`. Clientes que mandam JSON enviam o token na query
(`?csrf_token=...`), porque a validação lê o formulário combinado. A única
exceção é `POST /api/whatsapp/webhook`, isento porque tem assinatura própria.

**Trava de senha padrão (administrador).** Enquanto `users.password_changed_at`
for nulo (senha semeada ainda vigente), qualquer rota administrativa exige a troca:

- rotas HTML respondem `302` para `/settings?forced=1`;
- rotas de API (path `/api/` ou `?json=true`/`Accept: application/json`)
  respondem `403` com `{"error":"Troca de senha obrigatória."}`;
- ficam liberadas `/login`, `/logout`, `/settings`, `/api/health`, `/portal*`,
  `/validar*` e os estáticos.

**Seleção de JSON.** Uma rota com as duas saídas devolve JSON quando o path
começa com `/api/`, ou quando `?json=true`, ou quando `Accept` contém
`application/json`; caso contrário devolve HTML (e, quando não autenticado, `302`
para `/login`).

**Erros padronizados.** Os handlers traduzem o erro de domínio assim
(`internal/infrastructure/web/errors.go`):

| Situação | Status | Mensagem |
|---|---|---|
| `domain.ErrNotFound` | `404` | `Registro não encontrado.` |
| `domain.ErrConflict` | `409` | `Operação não permitida pelo estado atual do registro.` |
| `domain.ErrUnauthorized` | `401` | `Não autorizado.` |
| erro de validação | `400` | a mensagem do domínio (vai literal ao usuário) |
| qualquer outro | `500` | `Erro interno do servidor.` (o detalhe só vai para o log, com o `requestId`) |

Em JSON o corpo é `{"error":"...","requestId":"..."}`; o `requestId` é o mesmo
protocolo de 8 caracteres do log da requisição e da trilha de auditoria. Rota
inexistente responde `404` com `Rota não encontrada.` (JSON) ou a página de erro
com `Página não encontrada.` (HTML).

**Rate limit de login.** As três camadas em memória (por IP, global e bloqueio
progressivo) valem para `/login` e `/portal/login`. A recusa é sempre `429` com o
cabeçalho `Retry-After` em segundos e uma destas mensagens:

- `Muitas tentativas. Aguarde 1 minuto para tentar novamente.` (limite por IP ou global);
- `Muitas tentativas. Novas tentativas bloqueadas por N minuto(s).` (IP bloqueado por falhas repetidas).

---

## 2. Sistema e healthcheck

### `GET /healthz`
Liveness. Não toca o banco — um banco lento não pode reiniciar um processo saudável.
- **Autenticação**: nenhuma.
- **Resposta**: `200 OK`, `text/plain`, corpo `ok`.

### `GET /readyz`
Readiness. Só passa com ping real no banco (timeout de 2 s).
- **Autenticação**: nenhuma.
- **Resposta**: `200 OK`, `text/plain`, corpo `ready`; `503` com `banco indisponível` quando o ping falha.

### `GET /api/health`
Estado operacional do runtime.
- **Autenticação**: nenhuma.
- **Resposta**: `200 OK` (JSON)

```json
{
  "status": "ok",
  "runtime": "go",
  "framework": "net/http",
  "database": "sqlite (modernc)",
  "orm": "sqlc",
  "timestamp": "2026-09-21T15:09:00Z"
}
```

`database` é `sqlite (modernc)` ou `postgres (pgx)` conforme `DATABASE_URL`
(dialeto desconhecido sai como `desconhecido (<dialeto>)`). `timestamp` é
RFC 3339 em UTC.

### `GET /api/info`
Alimenta o modal **Informações** do painel: identificação do build e estado geral.
- **Autenticação**: sessão do administrador (`401` JSON sem ela).
- **Resposta**: `200 OK` (JSON)

```json
{
  "app": "Aluxen",
  "version": "1.0.61",
  "packageVersion": "1.0.0",
  "buildNumber": 61,
  "runtime": "Go",
  "nodeEnv": "production",
  "timezone": "America/Sao_Paulo",
  "startedAt": "2026-09-21T15:09:00Z",
  "uptimeSeconds": 3600,
  "database": { "dialect": "sqlite", "path": "/data/aluxen.db", "sizeBytes": 266240 },
  "counts": { "tenants": 36, "properties": 36, "leases": 36, "receipts": 77 },
  "appUrl": "https://aluxen.evolucsia.com",
  "user": "passoz",
  "authenticityUrl": "https://aluxen.evolucsia.com"
}
```

- `version` é `MAJOR.MINOR` de `internal/infrastructure/buildinfo` com o
  `buildNumber` como subversão (`1.0.61`); `nodeEnv` mantém o nome do app antigo e
  traz `APP_ENV`.
- `startedAt` é ISO 8601 em UTC; `uptimeSeconds` vem do `buildinfo`.
- `appUrl` é o valor do ajuste `app_url`; `authenticityUrl` é a URL pública
  resolvida da requisição (usada pelos QR Codes e links de validação).
- O commit, a branch, a imagem e a execução do GitHub que geraram o build são
  renderizados no modal a partir do `buildinfo`, não neste JSON.

---

## 3. Autenticação do administrador

### `GET /`
- **Autenticação**: nenhuma.
- **Resposta**: `302` para `/dashboard?tab=dashboard` com sessão, ou `302` para `/login` sem ela.

### `GET /login`
Renderiza o formulário de acesso (HTML, com o campo oculto de CSRF).
- **Autenticação**: nenhuma. Com sessão, `302` para `/dashboard?tab=dashboard`.

### `POST /login`
- **Corpo**: `application/x-www-form-urlencoded` ou `multipart/form-data`
  - `username` (obrigatório)
  - `password` (obrigatório)
  - `csrf_token` (obrigatório — o cookie de CSRF já vem da tela)
- **Respostas**:
  - `302 Found`: sucesso — define `__aluxen_session` e redireciona para
    `/dashboard?tab=dashboard`; quando a senha semeada ainda vigora, para
    `/settings?forced=1`.
  - `400 Bad Request`: `Não foi possível ler os dados enviados.` (corpo ilegível) ou
    `Informe usuário e senha.` (campo em branco).
  - `401 Unauthorized`: `Usuário ou senha inválidos.` (mensagem única, sem revelar
    se o usuário existe). A falha conta para o gate de tentativas.
  - `429 Too Many Requests`: limite por IP, teto global ou bloqueio progressivo,
    sempre com `Retry-After` e a mensagem correspondente (seção 1).
- **Auditoria**: `auth.login`, `auth.login-failed` (com o motivo da recusa e o usuário tentado) e `auth.logout`.

### `POST /logout` ou `GET /logout`
- **Autenticação**: sessão (sem ela, ainda assim limpa o cookie).
- **Resposta**: `302 Found` para `/login`, com o cookie de sessão removido.
- **Auditoria**: `auth.logout` quando havia sessão.

---

## 4. Painel e mutações de negócio

### `GET /dashboard`
Casca do painel: renderiza a aba pedida no servidor ou devolve os dados brutos.
- **Autenticação**: sessão do administrador.
- **Query params**:

| Param | Descrição |
|---|---|
| `tab` | aba aberta; valor desconhecido cai para `dashboard` |
| `month` / `year` | competência dos filtros da aba |
| `status` | filtro de situação (contratos e lançamentos); `all` ou vazio mostra todos |
| `toast` | aviso exibido após redirecionamento (ex.: `importado`) |
| `json` | `true` devolve JSON em vez do HTML |

- **Abas**: `dashboard`, `tenants`, `properties`, `leases`, `receipts`, `season`,
  `calendar`, `review`, `intake`, `conversations`, `adjustment`, `history`,
  `metrics`. Cada aba carrega o próprio contexto — o painel não paga I/O de tela
  que o locador não abriu.
- **Resposta**: `200 OK` HTML, ou JSON quando `json=true` (ou `Accept: application/json`):

```json
{
  "ok": true,
  "user": { "id": "01a0...", "username": "passoz" },
  "tenants": [ { "id": "...", "name": "...", "email": "...", "phone": null, "cpf": null } ],
  "properties": [ { "id": "...", "name": "...", "address": "...", "city": "...", "state": "...", "zip": null } ],
  "leases": [ {
    "id": "...", "tenant_id": "...", "property_id": "...",
    "start_date": "2026-01-10", "end_date": "2027-01-09", "due_day": 10,
    "rent_amount": 2500, "status": "active",
    "tenant_name": "...", "property_name": "...",
    "deposit_amount": 2500, "deposit_status": "held"
  } ],
  "receipts": [ {
    "id": "...", "lease_id": "...", "tenant_id": "...", "property_id": "...",
    "category": "rent", "description": null, "due_date": "2026-09-10",
    "paid_at": null, "amount": 2500, "paid_amount": null, "generated_key": null,
    "tenant_name": "...", "property_name": "...",
    "status": "due", "status_label": "A vencer"
  } ],
  "metrics": { "totals": {}, "monthly": [], "status": [], "categories": [], "properties": [], "topTenants": [] }
}
```

As chaves repetem as do app antigo: **snake_case** nos campos que espelham o
banco (`tenant_id`, `due_day`, `paid_amount`, `status_label`) e camelCase onde o
original já usava (`topTenants`). Os campos anuláveis saem como `null`;
`deposit_amount`/`deposit_status` só aparecem quando o contrato tem caução
registrada. `status` do lançamento é derivado no momento da resposta
(`paid`, `partial`, `overdue`, `due`) e `status_label` traz o rótulo em português.
Cada locatário traz `password_changed_at` (nulo = ainda usa a senha padrão da
Área do Cliente). O bloco `metrics` é o mesmo payload de `GET /metrics`.
- **Erros**: `401` JSON `Não autorizado.` sem sessão (ou `302 /login` em HTML);
  `403` `Troca de senha obrigatória.` com a senha semeada vigente.

### `POST /dashboard`
Despacha os intents de mutação do painel. Identificado pelo campo `intent`; o
handler é fino e devolve o erro de domínio.
- **Autenticação**: sessão do administrador + CSRF.
- **Corpo**: formulário (`application/x-www-form-urlencoded` ou `multipart/form-data`)
  com `intent` e os campos da área.
- **Resposta**: `200 OK` com `{"ok":true}` ou o erro padronizado (seção 1).
  Corpo ilegível → `400` `Não foi possível ler os dados enviados.`;
  intent desconhecido → `400` `Ação inválida.`
- Todo intent concluído registra a operação na trilha de auditoria com autor,
  data/hora, IP, protocolo da requisição e o diff campo a campo.

#### Intents de cadastro (`tenant.*`, `property.*`)

| Intent | Campos |
|---|---|
| `tenant.create` | `name`, `email`, `phone`, `cpf` |
| `tenant.update` | `id` + os campos acima |
| `tenant.delete` | `id` |
| `property.create` | `name`, `address`, `city`, `state`, `zip` |
| `property.update` | `id` + os campos acima |
| `property.delete` | `id` |

Validações: e-mail no formato `usuario@dominio.tld` → `400`
`Informe um e-mail válido.`; cadastro incompleto → `400`
`Preencha nome, endereço, cidade e estado.`; exclusão de locatário com contrato
ativo ou de imóvel com contrato vinculado é recusada (o vínculo é verificado na
tela, que não oferece a ação). Registro ausente na alteração/exclusão →
`400` `Registro não informado.`

#### Intents de contrato (`lease.*`)

| Intent | Campos |
|---|---|
| `lease.create` | `tenant_id`, `property_id`, `start_date`, `end_date`, `due_day`, `rent_amount`, `status` |
| `lease.update` | `id` + os campos acima |
| `lease.terminate` | `id` |
| `lease.suspend` | `id` |
| `lease.reactivate` | `id` |
| `lease.delete` | `id` |

- `start_date`/`end_date` em `YYYY-MM-DD`; `due_day` inteiro de 1 a 31;
  `rent_amount` aceita o decimal cru (`2500`, `2500.5`) ou a máscara pt-BR
  (`2.500,00`).
- Situações gravadas: `active` (Ativo), `suspended` (Suspenso), `season`
  (Temporada) e `terminated` (Encerrado). Na criação aceitam-se `active`,
  `suspended` e `season`; na edição também `terminated`.
- Erros literais do domínio: `Preencha todos os campos do contrato.`,
  `Informe o período do contrato.`,
  `A data de término deve ser igual ou posterior à data de início.`,
  `Informe um dia de vencimento entre 1 e 31.`, `Informe o valor do aluguel.`,
  `Status de contrato inválido.`, `Contrato não informado.`,
  `Este imóvel já possui contrato vigente no período.` e, para temporada,
  `Este imóvel já possui contrato vigente no período. Temporada só pode ser criada em imóvel sem contrato ativo ou com contrato suspenso.`

#### Intents de lançamento (`receipt.*`)

| Intent | Campos |
|---|---|
| `receipt.create` | `lease_id`, `tenant_id`, `property_id`, `category`, `description`, `due_date`, `amount`, `paid_at`, `paid_amount` |
| `receipt.update` | `id` + os campos acima |
| `receipt.pay` | `id`, `pay_amount`, `paid_at` |
| `receipt.delete` | `id` |
| `receipt.batch-pay` | `ids` (lista separada por vírgula) |
| `receipt.batch-delete` | `ids` (lista separada por vírgula) |
| `receipt.generate-monthly` | gera os lançamentos dos contratos ativos sem recibo da competência |

- `category` é `rent` (aluguel) ou `extra` (cobrança avulsa, com
  `description`); `pay_amount` aceita o valor cru ou a máscara pt-BR.
- Erros literais: `Preencha vencimento e valor.`,
  `Selecione um contrato para recebimento de aluguel.`,
  `Informe a descrição para cobrança avulsa.`, `Informe o valor pago.`,
  `Nenhum recibo selecionado.`, `Registro não informado.`

### Intents da temporada, das conversas e dos comprovantes

Ficam em rotas próprias (seções 8, 10 e 9 respectivamente), porque recebem
arquivos ou contexto diferente do formulário do painel:
`POST /dashboard/deposit`, `POST /dashboard/inspection` e
`POST /dashboard/conversation`.

### `GET /metrics`
Indicadores agregados do painel.
- **Autenticação**: sessão do administrador.
- **Resposta**: `200 OK` (JSON) com `totals`, `monthly[]`, `status[]`,
  `categories[]`, `properties[]` e `topTenants[]`. `totals` traz contagens
  (`tenants`, `properties`, `activeLeases`, `terminatedLeases`, `receipts`,
  `paidReceipts`, `partialReceipts`, `dueReceipts`, `overdueReceipts`) e valores
  (`totalAmount`, `paidAmount`, `openAmount`, `overdueAmount`, `expectedMonthly`).

---

## 5. Configurações da instância

### `GET /settings`
Renderiza a tela de configurações (dados do locador, URL pública, canal de
WhatsApp e troca de senha). É **isenta** da trava de senha, porque é justamente
onde a troca obrigatória acontece.
- **Autenticação**: sessão do administrador.
- **Resposta**: `200 OK` HTML. `?forced=1` mantém a tela em modo de troca
  obrigatória; a tela marca `Forced` também quando o administrador ainda usa a
  senha semeada.

### `POST /settings`
Executa os intents da tela. A resposta é sempre a própria página renderizada
(com `Message` de sucesso ou `Error` de recusa), não JSON.
- **Autenticação**: sessão do administrador + CSRF.
- **Intents**:

| Intent | Campos | Sucesso |
|---|---|---|
| `save-landlord` | `name`, `cpf`, `email` | `200` — `Dados salvos com sucesso.` |
| `save-app-url` | `app_url` | `200` — `URL pública do sistema salva com sucesso.` |
| `change-password` | `current_password`, `new_password`, `confirm_password` | `200` — `Senha alterada com sucesso.`; quando a troca era obrigatória, `302` para `/dashboard?tab=dashboard` |

- **Erros**:
  - `400` com o texto do domínio: `Informe o nome do locador.`,
    `Informe um e-mail válido.` (na validação dos dados do locador),
    `Informe uma URL começando por http:// ou https://.` (URL pública),
    `Senha atual incorreta.`,
    `A confirmação não corresponde à nova senha.`,
    `Nova senha deve ter no mínimo 6 caracteres.`, `Usuário não encontrado.`;
  - `400` `Ação inválida.` para intent desconhecido;
  - `400` `Não foi possível ler os dados enviados.` para corpo ilegível.
- **Auditoria**: alterações dos dados do locador, da URL pública e a troca de
  senha (`user.password`).

---

## 6. Métricas, histórico e backup

### `GET /audit`
Trilha de auditoria paginada em JSON. É um endpoint JSON: sessão ausente vira
`401 {"error":"Não autorizado."}`, nunca redirecionamento para o login.
- **Autenticação**: sessão do administrador.
- **Query params**:

| Param | Padrão | Descrição |
|---|---|---|
| `page` | `1` | página solicitada |
| `pageSize` | `25` | eventos por página (mínimo 5, máximo 200) |
| `order` | `desc` | `asc` inverte a ordem cronológica |
| `entity` | — | área (`tenant`, `property`, `lease`, `receipt`, `setting`, `user`, `document`, `whatsapp`, `backup`, `deposit`, `inspection`, `payment-proof`, `document-intake`, `collection`) |
| `action` | — | ação exata (ex.: `receipt.pay`, `document.receipt`) |
| `actor` | — | nome do autor |
| `from` / `to` | — | intervalo de datas `YYYY-MM-DD`, inclusivo, no fuso do servidor |
| `search` | — | busca textual em resumo, registro, autor e identificador |

- **Resposta**: `200 OK` (JSON)

```json
{
  "items": [
    {
      "id": "01a0a2f8-...",
      "createdAt": "2026-09-14T09:42:11.503",
      "actorId": "01a08355-...",
      "actorName": "passoz",
      "action": "receipt.pay",
      "entity": "receipt",
      "entityId": "01a09fee-...",
      "entityLabel": "Clarice Lispector · Casa das Dunas · 09/2026",
      "summary": "Baixa de Clarice Lispector · Casa das Dunas · 09/2026 registrada",
      "changes": [
        { "field": "paidAt", "label": "Pago em", "from": null, "to": "10/09/2026 09:42" }
      ],
      "metadata": {},
      "origin": "web",
      "ip": "192.168.0.10",
      "userAgent": "Mozilla/5.0 ...",
      "requestId": "70580d14"
    }
  ],
  "total": 128,
  "page": 1,
  "pageSize": 25,
  "actors": ["passoz", "Maria"]
}
```

`createdAt` usa o horário local do servidor. `changes` traz valores já
formatados para leitura (moeda em BRL, datas em `dd/mm/aaaa`, situações
traduzidas) e `metadata` guarda o contexto do evento (competência gerada, lote,
protocolo de envio, motivo de falha). Senhas, tokens e cookies nunca são gravados.

### `GET /audit/export`
Baixa a trilha em CSV com os mesmos filtros de `GET /audit`.
- **Autenticação**: sessão do administrador.
- **Resposta**: `200 OK` com `Content-Type: text/csv; charset=utf-8` e
  `Content-Disposition: attachment; filename="aluxen-historico-YYYY-MM-DD.csv"`.
- CSV com BOM UTF-8, delimitador `;`, quebras CRLF e **13 colunas** (*Data/Hora,
  Usuário, Ação, Área, Registro, ID do registro, Resumo, Alterações, Metadados,
  Origem, IP, User-Agent, Request*), truncado em 5.000 linhas. `Alterações` sai
  como `Rótulo: antes → depois` e `Metadados` como `Rótulo: valor | ...`.
- A exportação é registrada na trilha (`history.export`).

### `GET /export`
Backup completo dos cadastros.
- **Autenticação**: sessão do administrador.
- **Resposta**: `200 OK` com `Content-Type: application/json` e
  `Content-Disposition: attachment; filename="aluxen-backup-YYYY-MM-DD.json"`,
  corpo `{"exportedAt","tenants","properties","leases","receipts"}`.
- O backup carrega apenas os cadastros: **hash de senha e credenciais da Área do
  Cliente ficam de fora**.
- Registra `backup.export` na trilha.

### `POST /import`
Restaura um backup.
- **Autenticação**: sessão do administrador + CSRF.
- **Corpo**: `multipart/form-data` com o campo **`file`** (JSON). Tamanho máximo
  `MaxBackupImportSize` (padrão 10 MiB).
- **Respostas**:
  - `302 Found` para `/dashboard?tab=dashboard&toast=importado` no sucesso
    (registra `backup.import`);
  - `400` em texto simples: `Arquivo muito grande.`, `Arquivo não enviado.`,
    `Não foi possível ler o arquivo.`, `Arquivo JSON inválido.`,
    `Formato de backup inválido.` ou `Não foi possível importar os dados.`

---

## 7. Documentos em PDF e prova pública

Os PDFs são montados em modelo tipado (`internal/infrastructure/pdf`) e
desenhados em A4. O selo de autenticidade é o token HMAC do documento, no QR Code
e na chave impressa (`AUTH-XXXX-XXXX-XXXX-XXXX`). Ver `docs/VALIDACAO-GO.md`.

### `GET /pdf`
Emite o Recibo de Pagamento de Aluguel.
- **Autenticação**: sessão do administrador **ou** `token` válido **ou** sessão do
  locatário titular do recibo.
- **Query params**: `id` (obrigatório), `token` (opcional com sessão),
  `assinatura` (opcional — `false` emite o recibo sem a imagem da assinatura do
  locador; a assinatura impressa é visualizada e **não** é assinatura eletrônica
  com validade ICP-Brasil).
- **Resposta**: `200 OK`, `Content-Type: application/pdf`,
  `Content-Disposition: inline; filename="recibo-<id>.pdf"`.
- **Erros**:
  - `400` `ID inválido`;
  - `404` `Recibo não encontrado`;
  - `403` `Recibo não pago. O recibo só é liberado após o pagamento.` (lançamento
    pendente/vencido não gera documento);
  - `403` `Este documento não pertence ao seu cadastro.` (sessão de locatário sem
    vínculo) e `401`/`302` para quem não está logado e não tem token.
- **Auditoria**: `document.receipt` (origem `web` quando há sessão, `public`
  quando o acesso veio do token).

### `GET /contract-pdf`
Emite o Contrato de Locação Residencial (25 cláusulas, com temporada prevista).
- **Autenticação**: sessão do administrador **ou** `token` válido **ou** sessão do
  locatário titular do contrato.
- **Query params**: `id` (obrigatório), `token` (opcional com sessão).
- **Resposta**: `200 OK`, `application/pdf`,
  `Content-Disposition: inline; filename="contrato-<id>.pdf"`.
- **Erros**: `400` `ID inválido`; `404` `Contrato não encontrado`; `403`
  `Este documento não pertence ao seu cadastro.`
- **Auditoria**: `document.contract`.

### `GET /validar/recibo/{id}?token=...`
Página pública de validação do recibo.
- **Autenticação**: pública — o `token` da URL é a credencial.
- **Respostas**:
  - `200 OK`: selo verde, dados do recibo e botão para o PDF original;
  - `400`: `Chave de autenticação inválida ou documento não autenticado.`;
  - `404`: `Recibo não localizado na base oficial de registros.`;
  - `403`: `Recibo ainda não pago. O documento só é liberado após o pagamento.`
- A consulta é datada em `dd/mm/aaaa HH:mm` no fuso do servidor.

### `GET /validar/contrato/{id}?token=...`
Página pública de validação do contrato.
- **Autenticação**: pública.
- **Respostas**: `200 OK`; `400` `Chave de autenticação inválida ou documento não autenticado.`;
  `404` `Contrato não localizado na base oficial de registros.`

---

## 8. Temporada: caução e vistoria

### `POST /dashboard/deposit`
Registra a caução de um contrato ou devolve o valor retido.
- **Autenticação**: sessão do administrador + CSRF.
- **Intents**:

| Intent | Campos |
|---|---|
| `deposit.set` | `lease_id`, `deposit_amount`, `deposit_received_at`, `deposit_notes` |
| `deposit.refund` | `lease_id`, `refund_amount`, `refund_notes` |

- **Resposta**: `200 OK` `{"ok":true}`; intent desconhecido → `400` `Ação inválida.`;
  corpo ilegível → `400` `Não foi possível ler os dados enviados.`
- **Erros literais**: `Informe o valor.` / `Informe um valor válido.` (campo
  monetário), `Contrato não encontrado.`, `Informe o valor da caução.`,
  `A caução deste contrato já foi devolvida.`,
  `Este contrato não tem caução retida para devolver.`,
  `O valor devolvido não pode ser negativo.`,
  `O valor devolvido não pode ser maior que a caução retida.`
- **Situações da caução**: `none`, `held`, `refunded`, `partially_refunded`,
  `forfeited`.

### `POST /dashboard/inspection`
Cria, altera ou exclui um auto de vistoria.
- **Autenticação**: sessão do administrador + CSRF.
- **Corpo**: `multipart/form-data` (teto de 40 MiB)

| Campo | Descrição |
|---|---|
| `intent` | `inspection.create`, `inspection.update` ou `inspection.delete` |
| `id` | vistoria (update/delete) |
| `lease_id` | contrato vistoriado |
| `kind` | `checkin` (entrada) ou `checkout` (saída) |
| `performed_at` | data/hora da realização |
| `condition` | `otima`, `boa`, `regular` ou `ruim` |
| `notes` | observações |
| `item_area[]`, `item_name[]`, `item_state[]`, `item_notes[]` | itens conferidos, em campos repetidos paralelos |
| `photos[]` | até 10 fotos, 8 MiB cada |

- **Resposta**: `200 OK` `{"ok":true}`.
- **Erros**: `400` `Não foi possível ler os dados enviados.` (corpo ilegível),
  `Envie no máximo 10 fotos por vistoria.`,
  `Cada foto pode ter no máximo 8 MB.`,
  `Não foi possível ler a foto enviada.`,
  `Formato de foto não suportado. Use JPG, PNG ou WEBP.`,
  `Informe o tipo da vistoria (entrada ou saída).`,
  `Informe o estado geral do imóvel.`, `Informe a data da vistoria.`,
  `Contrato não encontrado.`

### `GET /inspection-pdf?id=&token=`
Emite o auto de vistoria em PDF (itens conferidos e fotos anexadas).
- **Autenticação**: como os demais documentos (administrador, token HMAC ou
  locatário titular do contrato).
- **Resposta**: `200 OK`, `application/pdf`,
  `Content-Disposition: inline; filename="vistoria-<id>.pdf"`.
- **Erros**: `400` `ID inválido`; `404` `Vistoria não encontrada`; `403`
  `Este documento não pertence ao seu cadastro.`
- **Auditoria**: `document.inspection`.

### `GET /deposit-pdf?id=&token=`
Emite o recibo de devolução de caução em PDF.
- **Autenticação**: administrador, token HMAC ou locatário titular.
- **Resposta**: `200 OK`, `application/pdf`,
  `Content-Disposition: inline; filename="caucao-<id>.pdf"`.
- **Erros**: `400` `ID inválido`; `404` `Caução não encontrada`; `403`
  `Este contrato não tem caução registrada.`
- **Auditoria**: `document.deposit`.

### `GET /validar/vistoria/{id}?token=...`
Página pública de validação do auto de vistoria.
- **Respostas**: `200 OK`; `400`
  `Chave de autenticação inválida ou documento não autenticado.`; `404`
  `Vistoria não localizada na base oficial de registros.`

### `GET /validar/caucao/{id}?token=...`
Página pública de validação do recibo de devolução de caução.
- **Respostas**: `200 OK`; `400`
  `Chave de autenticação inválida ou documento não autenticado.`; `404`
  `Caução não localizada na base oficial de registros.`

---

## 9. Comprovantes e onboarding por documento

### `POST /api/payments/proof`
Recebe um comprovante de pagamento, guarda o arquivo e enfileira a leitura
assistida por IA. A resposta **não** espera o provedor: devolve o protocolo do
trabalho. A baixa só acontece com validação determinística; o que não fecha vai
para a fila de revisão humana.
- **Autenticação**: sessão do administrador + CSRF.
- **Corpo**: `multipart/form-data`

| Campo | Descrição |
|---|---|
| `file` | imagem ou PDF, até 10 MiB |
| `source` | `whatsapp`, `portal` ou `panel` |
| `phone` | telefone do remetente (opcional) |
| `tenant_id` | locatário vinculado (opcional) |

- **Resposta**: `200 OK`

```json
{ "ok": true, "proofId": "...", "jobId": "...", "status": "pending" }
```

`status` é a situação do comprovante: `pending` (Lendo), `accepted` (Aceito),
`review` (Em revisão) ou `rejected` (Recusado).
- **Erros**: `400` `Envie o arquivo do comprovante.`,
  `Não foi possível ler os dados enviados.` (multipart inválido),
  `O comprovante pode ter no máximo 10 MB.`,
  `Não foi possível ler o arquivo enviado.`,
  `Formato de comprovante não suportado. Envie uma imagem ou um PDF.`,
  `Inquilino não encontrado.`

### `GET /api/payments/proof/{id}/file`
Entrega o arquivo do comprovante para a revisão à vista.
- **Autenticação**: sessão do administrador (o comprovante é dado pessoal; não há
  acesso por locatário nem por token).
- **Resposta**: `200 OK` com `Content-Type` de imagem (`image/jpeg`,
  `image/png`, `image/webp`, `image/heic`, `image/heif`) ou `application/pdf`,
  `Content-Disposition: inline` e `Cache-Control: private, no-store`. O tipo
  efetivo sai do `mime` gravado; quando ele não é imagem nem PDF, é inferido pela
  extensão — nunca é servido tipo arbitrário.
- **Erros**: `404` `Arquivo do comprovante não encontrado.` (registro ausente,
  arquivo fora do armazenamento ou tipo não servível); `404` `Registro não encontrado.`

### `GET /api/payments/review`
Devolve a fila de revisão humana.
- **Autenticação**: sessão do administrador.
- **Query params**: `limit` (padrão 50; valores inválidos caem no padrão).
- **Resposta**: `200 OK` `{"ok":true,"proofs":[...]}` — cada linha traz o
  comprovante (`source`, `phone`, `tenantName`), o que a leitura extraiu
  (`extractedAmount`, `extractedDate`, `extractedPayer`, `extractedPayee`,
  `extractedIdentifier`, `confidence`/`confidenceLabel`), o lançamento candidato
  (`receiptId`, `receiptLabel`, `receiptAmount`, `receiptDueDate`, `receiptPaid`)
  e o `status`/`reason`.

### `POST /api/payments/review`
Registra a decisão humana sobre um comprovante ambíguo (aceitar ou recusar).
- **Autenticação**: sessão do administrador + CSRF.
- **Corpo**: formulário do painel (`proof_id`, `approve`, `receipt_id`, `reason`)
  **ou** JSON com os mesmos nomes em camelCase (`proofId`, `approve`, `receiptId`,
  `reason`). `approve` aceita `1`, `true`, `on`, `yes`, `sim`, `accept`, `aceitar`.
- **Resposta**: `200 OK` `{"ok":true}`; a decisão é registrada na trilha com quem decidiu.
- **Erros**: `400` `Não foi possível ler os dados enviados.`,
  `Comprovante não informado.`, `Este comprovante já foi decidido.`,
  `Nenhum lançamento em aberto para dar baixa.`, `Informe o motivo da recusa.`

### `POST /api/intake/document`
Onboarding por documento: recebe contrato antigo, documento de identidade ou
comprovante de residência e cria uma **proposta** de cadastro (`pending`). Nada é
gravado como cadastro antes da conferência humana.
- **Autenticação**: sessão do administrador + CSRF.
- **Corpo**: `multipart/form-data` com `file` (até 20 MiB) e `kind`
  (`lease`, `id-document` ou `address-proof`).
- **Resposta**: `200 OK` `{"ok":true,"intakeId":"...","jobId":"..."}`.
- **Erros**: `400` `O arquivo pode ter no máximo 20 MB.`,
  `Não foi possível ler o arquivo enviado.`, `Envie o arquivo do documento.`,
  `Envie o arquivo do documento para a leitura.`,
  `Tipo de documento não aceito.`,
  `Formato de arquivo não suportado. Envie PDF, JPG, PNG ou WEBP.`,
  `Armazenamento de arquivos indisponível nesta instância.`

### `POST /api/intake/confirm`
Aplica a conferência humana: cria o que foi aprovado (locatário, imóvel e/ou
contrato) ou descarta a proposta com o motivo.
- **Autenticação**: sessão do administrador + CSRF.
- **Corpo**: formulário do painel (urlencoded) **ou** JSON, com os campos:
  `intakeId`, `kind`, `action` (`discard` descarta; qualquer outro valor confirma),
  `reason`, `tenantId`, `propertyId`, `createTenant`, `createProperty`,
  `createLease`, `tenant_name`, `tenant_cpf`, `tenant_email`, `tenant_phone`,
  `property_name`, `property_city`, `property_state`, `start_date`, `end_date`,
  `due_day`, `rent_amount`, `adjust_index`, `guarantee`, `name`, `cpf`, `email`,
  `phone`, `address`, `city`, `state`, `zip`.
  Os sinalizadores aceitam `1`, `true`, `on`, `sim`, `yes` (ou booleano no JSON).
- **Resposta**: `200 OK` — na confirmação
  `{"ok":true,"intakeId":"...","tenantId":"...","propertyId":"...","leaseId":"..."}`;
  no descarte `{"ok":true,"intakeId":"...","status":"discarded"}`.
- **Erros**: `400` `Não foi possível ler os dados enviados.`,
  `Esta proposta já foi conferida.`, `Tipo de documento não aceito.`,
  `Escolha o que criar a partir do documento.`,
  `Locatário informado não encontrado.`, `Imóvel informado não encontrado.`,
  `Para criar o contrato, informe o locatário e o imóvel.`,
  `Informe o motivo do descarte.`, `Informe um dia de vencimento válido.`,
  `Documento não informado.`, `Proposta de cadastro não encontrada.`,
  `Documento de identidade não descreve imóvel.`

---

## 10. Conversas, régua de cobrança e tier 2 assistido

### `GET /api/conversations`
Conversas do WhatsApp por locatário, da mais recente para a mais antiga.
- **Autenticação**: sessão do administrador.
- **Query params**: `limit` (janela de mensagens lida por conversa; `0`/ausente
  usa o padrão).
- **Resposta**: `200 OK` `{"ok":true,"conversations":[...]}` — cada conversa traz
  `tenantId`, `tenantName`, `phone`, `lastMessage`, `lastDirection`/
  `lastDirectionLabel`, `lastAt`, `messageCount`, `awaitingReply`,
  `overdueCount`, `overdueTotal` e, quando há atraso, `overdue` com o lançamento,
  os dias de atraso, o estágio atual e o último estágio enviado.
- **Erros**: `400` `Limite inválido.`

### `POST /dashboard/conversation`
Resposta manual do locador e escalonamento da conversa.
- **Autenticação**: sessão do administrador + CSRF.
- **Intents**:

| Intent | Campos | Resposta |
|---|---|---|
| `conversation.reply` | `tenant_id`, `message` | `200` `{"ok":true,"jobId":"..."}` |
| `conversation.escalate` | `tenant_id`, `reason` | `200` `{"ok":true}` |

- O destinatário da resposta é sempre o telefone do cadastro do titular, nunca o
  formulário. Sem canal montado na instância: `503` `Canal de WhatsApp indisponível.`
- **Erros**: `400` `Não foi possível ler os dados enviados.` e `Ação inválida.`;
  `400` `O inquilino <nome> não tem telefone cadastrado.` quando falta telefone.

### `POST /api/collection/run`
Avança a régua dos lançamentos vencidos.
- **Autenticação**: sessão do administrador + CSRF.
- **Corpo**: `dry_run` (formulário — `1`/`true`/`on`/`sim`/`yes` — ou JSON
  booleano `dry_run`). Com `dry_run`, nada é enviado e nada é registrado.
- **Resposta**: `200 OK` `{"ok":true,"dryRun":<bool>,"outcomes":[...]}`; cada
  desfecho traz `receiptId`, `tenantId`, `tenantName`, `stage`/`stageLabel`,
  `channel`, `delivered`, `dryRun`, `reason` e `message`. A lista sai vazia
  (`[]`) quando não há candidato.
- Os estágios são decididos pelo atraso do lançamento (nunca pelo modelo de
  linguagem): `1` Lembrete amigável (a partir de 1 dia), `2` Cobrança firme (7 dias)
  e `3` Proposta de parcelamento (15 dias).

### `GET /api/report/monthly`
Relatório da competência.
- **Autenticação**: sessão do administrador.
- **Query params**: `year` e `month` (sem eles, o mês corrente).
- **Resposta**: `200 OK` `{"ok":true,"report":{...}}` com `year`, `month`,
  `monthKey`, `label`, `receipts`, `expected`, `received`, `open`, `overdue`,
  `paidCount`, `counts[]` (as quatro situações), `byProperty[]` e `byTenant[]`.
- **Erros**: `400` `Informe um ano e um mês válidos.` (campo presente e ilegível —
  competência pedida nunca devolve silenciosamente outro mês).

### `POST /api/report/ask`
Responde a uma pergunta em linguagem natural pelas ferramentas tipadas de leitura.
- **Autenticação**: sessão do administrador + CSRF.
- **Corpo**: `question` (formulário urlencoded ou JSON).
- **Resposta**: `200 OK` `{"ok":true,"answer":{"question":"...","answer":"...","refused":false,"tools":["..."]}}`.
- **Erros**: `400` `Não foi possível ler os dados enviados.`, `Escreva a pergunta.`;
  `503` `A consulta em linguagem natural não está configurada nesta instância.`

### `POST /api/adjustment/preview`
Calcula o reajuste de um contrato sem gravar nada.
- **Autenticação**: sessão do administrador + CSRF.
- **Corpo**: `lease_id`, `index`, `rate` (aceita o valor normalizado, a máscara
  pt-BR e o sinal `%`), `effective_date`; JSON com os mesmos nomes.
- **Resposta**: `200 OK` `{"ok":true,"preview":{...}}` com `leaseId`,
  `tenantName`, `propertyName`, `index`/`indexLabel`, `rate`/`rateLabel`,
  `previousAmount`/`previousAmountLabel`, `newAmount`/`newAmountLabel`,
  `difference`/`differenceLabel` e `effectiveDate`/`effectiveDateLabel`.
- **Erros**: `400` `Não foi possível ler os dados enviados.`,
  `Escolha o índice do reajuste (IGP-M ou IPCA).`,
  `Selecione o contrato do reajuste.`,
  `Informe um percentual de reajuste maior que zero.`,
  `Informe um percentual de reajuste válido.`,
  `Informe a data de vigência do reajuste.` / `Informe uma data de vigência válida.`,
  `Contrato encerrado não pode ser reajustado.`,
  `O contrato não tem valor de aluguel para reajustar.`

### `POST /api/adjustment/apply`
Aplica o reajuste e devolve a notificação formal. Exige a confirmação explícita.
- **Autenticação**: sessão do administrador + CSRF.
- **Corpo**: os mesmos campos do preview, mais `confirm` (`1`/`true`/`on`/`sim`/
  `yes`, ou booleano/`0`/`1` no JSON).
- **Resposta**: `200 OK` `{"ok":true,"result":{"preview":{...},"notice":"...","noticeSource":"..."}}`
  e o reajuste entra na trilha de auditoria.
- **Erros**: os do preview, mais `Confirme o reajuste para aplicar.`

---

## 11. WhatsApp (WAHA)

O canal é o **WAHA**: pareamento, estado de sessão e envio são chamadas HTTP (não
há Chromium nem robô headless na instância). Sem `WAHA_API_URL`, todas as rotas
abaixo respondem `503` `Canal de WhatsApp indisponível.` Detalhes em
`docs/WHATSAPP-GO.md`.

### `GET /api/whatsapp/status`
- **Autenticação**: sessão do administrador.
- **Resposta**: `200 OK`

```json
{
  "isLoggedIn": true,
  "hasQRCode": false,
  "chromiumAvailable": true,
  "profileDir": ""
}
```

O nome `chromiumAvailable` vem do contrato antigo, mantido por paridade: agora
responde "o canal está configurado" (`true` quando `WAHA_API_URL` está definida).
`profileDir` fica vazio porque não há mais perfil de navegador na instância — a
sessão vive no WAHA.

### `POST /api/whatsapp/login/start`
Inicia o pareamento (sessão/pareamento no WAHA) e devolve o estado inicial.
- **Autenticação**: sessão do administrador + CSRF.
- **Resposta**: `200 OK` `{"ok":true,"state":{...}}`.

### `GET /api/whatsapp/login/status`
Estado do pareamento em curso.
- **Resposta**: `200 OK` `{"ok":true,"state":{"status":"scan_qr","qrCode":"data:image/png;base64,...","message":"...","error":"..."}}`.
- `status` é `idle`, `starting`, `scan_qr`, `syncing`, `connected`, `error` ou
  `timeout`; `qrCode` só vem quando o estado é `scan_qr` (imagem de 280 px).
  O pareamento expira em 3 minutos e as mensagens de apoio são as do app antigo
  (`Iniciando sessão do WhatsApp...`, `WhatsApp conectado com sucesso ao Aluxen!`,
  `Tempo limite de 3 minutos esgotado. Tente novamente.`,
  `Nenhum canal do WhatsApp configurado nesta instância. Defina WAHA_API_URL.`,
  `Não foi possível consultar a sessão do WhatsApp.`).

### `POST /api/whatsapp/login/cancel`
Cancela o pareamento em curso. **Resposta**: `200 OK` `{"ok":true,"state":{...}}`.

### `POST /api/whatsapp/logout`
Encerra a sessão pareada e registra `whatsapp.logout` na trilha.
- **Resposta**: `200 OK` `{"ok":true,"message":"WhatsApp desconectado com sucesso."}`;
  falha ao desconectar → `400` `Não foi possível desconectar o WhatsApp agora. Tente novamente.`

### `POST /api/whatsapp/preview`
Monta a mensagem e devolve o destinatário e o link direto.
- **Corpo**: `type` (`receipt` ou `lease`) e `id`.
- **Resposta**: `200 OK`

```json
{
  "ok": true,
  "type": "receipt",
  "id": "...",
  "phone": "11999998888",
  "formattedPhone": "5511999998888",
  "tenantName": "João da Silva",
  "propertyName": "Apto 302",
  "message": "Olá, *João da Silva*!...",
  "directUrl": "https://wa.me/5511999998888?text=..."
}
```

- **Erros**: `400` `Parâmetros inválidos.` (corpo ilegível, `id` vazio ou `type`
  fora de `receipt`/`lease`), `Recibo ainda não pago.` (o documento só existe
  depois da baixa), `Lançamento sem titular resolvido: não há para quem enviar.`,
  `Contrato sem titular resolvido: não há para quem enviar.`; id inexistente →
  `404` `Registro não encontrado.`

### `POST /api/whatsapp/send`
Inicia o envio pelo canal em segundo plano e responde de imediato com o
identificador do acompanhamento.
- **Corpo**: `to` (telefone) e `message` (texto).
- **Resposta**: `200 OK` `{"ok":true,"jobId":"..."}`; o envio registra
  `whatsapp.send` (e `whatsapp.send-failed` com o motivo, quando falha).

### `GET /api/whatsapp/send/status?jobId=...`
Andamento do envio criado por quem consulta.
- **Resposta**: `200 OK`

```json
{
  "ok": true,
  "job": {
    "id": "...", "userId": "...", "status": "done", "stage": "done",
    "result": { "success": true, "to": "5511999998888", "message": "...", "timestamp": "2026-09-21T14:35:00Z" },
    "createdAt": 1789000000000, "updatedAt": 1789000005000
  }
}
```

- `status` é `running`, `done` ou `error`; `stage` é `preparing`, `connecting`,
  `opening_chat`, `sending`, `confirming` ou `done`.
- Jobs vivem **5 minutos** após a última atualização e são por administrador: job
  de outro usuário (ou expirado) responde `404` `Registro não encontrado.`

### `POST /api/whatsapp/webhook`
Recebe os eventos do WAHA (mensagens recebidas e respostas do atendimento).
- **Autenticação**: assinatura HMAC do corpo no header **`X-Webhook-Hmac`**
  (SHA-256). É a única rota mutante **isenta de CSRF**; no modo demo, sem
  `WAHA_WEBHOOK_SECRET`, a política aceita o evento sem assinatura (com aviso no log).
- **Corpo**: envelope do WAHA (`event`, `session`, `payload`).
- **Respostas**:
  - `200 OK` `{"ok":true}` — mensagem registrada (vinculada ao locatário pelo
    telefone) e atendimento disparado;
  - `200 OK` `{"ok":true,"ignored":true}` — evento que não é mensagem (respondido
    para o WAHA não reentregar);
  - `400` `Corpo inválido.` — corpo ilegível ou payload não reconhecido;
  - `401` `Não autorizado.` — assinatura ausente ou inválida.
- A resposta do atendimento sai como job assíncrono no canal (o webhook não
  espera o envio terminar) e todo envio/falha é registrado na trilha.

---

## 12. Área do Cliente (portal do locatário)

Somente leitura para o locatário, isolada por titular, com sessão própria
(`__aluxen_client_session`). Um mesmo navegador pode manter as duas sessões
abertas sem interferência.

### `GET /portal/login`
Formulário de acesso (HTML). Com sessão de locatário, `302` para `/portal`.

### `POST /portal/login`
- **Corpo**: `cpf` (qualquer formatação: `529.982.247-25` ou `52998224725`) e
  `password` (senha pessoal; no primeiro acesso, o próprio CPF sem pontuação),
  mais o `csrf_token`.
- **Respostas**:
  - `302 Found`: sucesso — define `__aluxen_client_session` e redireciona para
    `/portal/password` (senha padrão ainda vigente) ou `/portal`;
  - `400` `Não foi possível ler os dados enviados.` ou `Informe CPF e senha.`;
  - `401` `CPF ou senha inválidos.` (mensagem única);
  - `429` com `Retry-After` e a mensagem do gate (seção 1).

### `POST /portal/logout` ou `GET /portal/logout`
Encerra a sessão e redireciona para `/portal/login`.

### `GET /portal`
Área do Cliente: contratos do titular (vigência, vencimento, aluguel e links para
PDF e autenticidade) e **apenas os recibos quitados** (integrais ou parciais).
- **Autenticação**: sessão do locatário.
- **Respostas**: `302` para `/portal/login` (sem sessão) ou `/portal/password`
  (senha padrão vigente); `200 OK` HTML. Com `?ok=senha`, exibe
  `Senha alterada com sucesso.`

### `GET /portal/password`
Formulário de troca de senha; marca o primeiro acesso quando a senha padrão ainda vale.

### `POST /portal/password`
- **Corpo**: `current_password`, `new_password`, `confirm_password`.
- **Regras**: a senha atual deve conferir (o CPF vale como senha atual no
  primeiro acesso); a nova senha tem no mínimo 6 caracteres, não pode ser o CPF
  do titular e precisa ser igual à confirmação.
- **Respostas**:
  - `302 Found` para `/portal?ok=senha`;
  - `400` `A confirmação não corresponde à nova senha.`,
    `Não foi possível ler os dados enviados.`, `Senha atual incorreta.`,
    `A nova senha deve ter no mínimo 6 caracteres.`,
    `A nova senha não pode ser o seu CPF.`
- **Auditoria**: os eventos da Área do Cliente saem com origem `client`.

---

## Apêndice: estações de erro por código

| Código | Quando |
|---|---|
| `302` | redirecionamentos de navegação (login, logout, portal, importação, troca obrigatória de senha) |
| `400` | validação de domínio (mensagem literal), corpo ilegível, arquivo grande ou formato não suportado, chave de autenticidade inválida |
| `401` | credenciais inválidas no login, requisição de API sem sessão |
| `403` | CSRF inválido, senha padrão vigente nas APIs, documento não pago, documento de outro titular, caução inexistente |
| `404` | registro/documento/job inexistente, rota não encontrada, comprovante sem arquivo servível |
| `409` | operação não permitida pelo estado atual do registro |
| `429` | rate limit de login (sempre com `Retry-After`) |
| `500` | falha não tratada — a resposta nunca vaza detalhe, só o `requestId` |
| `503` | banco indisponível na readiness, canal de WhatsApp não montado, consulta em linguagem natural não configurada |
