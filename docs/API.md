# Referência Completa da API e Rotas HTTP — Aluxen

Esta documentação cataloga todos os endpoints HTTP expostos pela aplicação Bun/Hono do Aluxen, incluindo parâmetros de entrada, tipos de conteúdo e formatos de resposta.

---

## Sumário de Rotas

1. [Sistema e Healthcheck](#1-sistema-e-healthcheck)
2. [Autenticação e Sessão](#2-autenticação-e-sessão)
3. [Painel e Mutações de Negócio](#3-painel-e-mutações-de-negócio)
4. [Configurações do Sistema](#4-configurações-do-sistema)
5. [Backup e Restauração](#5-backup-e-restauração)
6. [Emissão de Documentos em PDF](#6-emissão-de-documentos-em-pdf)
7. [Portal Público de Validação](#7-portal-público-de-validação)
8. [Integração e Automação do WhatsApp](#8-integração-e-automação-do-whatsapp)
9. [Histórico e Auditoria](#9-histórico-e-auditoria)
10. [Área do Cliente (Portal do Locatário)](#10-área-do-cliente-portal-do-locatário)

---

## 1. Sistema e Healthcheck

### `GET /api/health`
Retorna o estado operacional do runtime e serviços essenciais.
- **Autenticação**: Não requerida.
- **Resposta**: `200 OK` (JSON)
```json
{
  "status": "ok",
  "runtime": "bun",
  "framework": "hono",
  "database": "sqlite (bun:sqlite)",
  "orm": "drizzle-orm",
  "timestamp": "2026-09-08T12:00:00.000Z"
}
```

### `GET /api/info`
Alimenta o modal **Informações** do painel: identificação do build em execução e estado geral do sistema.
- **Autenticação**: Requerida (Sessão).
- **Resposta**: `200 OK` (JSON) — `401 Unauthorized` sem sessão.
```json
{
  "app": "Aluxen",
  "version": "1.0.0",
  "runtime": "Bun 1.4.2",
  "nodeEnv": "production",
  "timezone": "America/Sao_Paulo",
  "startedAt": "2026-09-21T15:09:00.000Z",
  "uptimeSeconds": 3600,
  "database": { "path": "/data/aluxen.db", "sizeBytes": 266240 },
  "counts": { "tenants": 36, "properties": 36, "leases": 36, "receipts": 77 },
  "appUrl": "https://aluxen.evolucsia.com",
  "build": {
    "version": "1.0.0",
    "commit": "90cd003c8ef0c4eec2369715470fc6ff3ca6eacfc61e3be316d132b1dc7969de",
    "shortCommit": "7f2ba68",
    "ref": "release",
    "buildTime": "2026-09-21T14:52:02Z",
    "image": "ghcr.io/passoz/aluxen:sha-7f2ba68",
    "runUrl": "https://github.com/passoz/aluxen/actions/runs/35615036183",
    "source": "image"
  }
}
```
- `build.source`: `image` (metadados injetados pelo workflow), `local` (execução de desenvolvimento com git) ou `unknown` (execução sem metadados, como uma imagem publicada antes desta funcionalidade).
- `startedAt` e `buildTime` são ISO 8601 em UTC; o painel exibe os dois convertidos para leitura com a marcação `(UTC)`.

---

## 2. Autenticação e Sessão

### `GET /`
Redireciona para `/dashboard?tab=tenants` se autenticado, ou `/login` se desautenticado.

### `GET /login`
Retorna a página de login renderizada via SSR.

### `POST /login`
Autentica o usuário com proteção de rate limit (máximo de 5 tentativas por minuto por IP por padrão).
- **Corpo**: `application/x-www-form-urlencoded` ou `multipart/form-data`
  - `username` (string, obrigatório)
  - `password` (string, obrigatório)
- **Respostas**:
  - `302 Found`: Sucesso (define o cookie `__aluxen_session` e redireciona para `/dashboard?tab=tenants`).
  - `401 Unauthorized`: Credenciais inválidas.
  - `429 Too Many Requests`: Limite de tentativas por minuto atingido.

### `POST /logout` ou `GET /logout`
Invalida o cookie de sessão e redireciona para `/login`.

---

## 3. Painel e Mutações de Negócio

### `GET /dashboard`
- **Autenticação**: Requerida (Sessão).
- **Query Params**:
  - `tab` (string, opcional): `'tenants'` | `'properties'` | `'leases'` | `'receipts'` (Padrão: `'tenants'`).
  - `month` (string, opcional): Mês no formato `MM` (ex.: `'09'`).
  - `year` (string, opcional): Ano no formato `YYYY` (ex.: `'2026'`).
  - `status` (string, opcional): Filtro de recibos (`'all'`, `'paid'`, `'pending'`, `'overdue'`).
  - `json` (boolean, opcional): Se `'true'`, retorna os dados brutos da tela em formato JSON.
- **Resposta**: HTML completo (SSR) ou payload JSON estruturado contendo listas de inquilinos, imóveis, contratos e recibos.

### `POST /dashboard` (Mutations)
Executa operações de escrita no sistema. Identificado pelo campo `intent`.

Toda mutação concluída com sucesso é registrada na **trilha de auditoria** (`audit_logs`) com autor, data/hora, IP, protocolo da requisição e o diff dos campos alterados (valores antes/depois). Ajustes que não alteram nenhum campo não geram evento.

#### Inquilinos (`tenants`):
- `intent: "tenant.create"`:
  - `name` (string, obrigatório)
  - `email` (string, obrigatório): validado no formato `usuario@dominio.tld`; endereços inválidos retornam `400` com *"Informe um e-mail válido."*
  - `phone` (string, opcional)
  - `cpf` (string, opcional)
- `intent: "tenant.update"`:
  - `id` (string, obrigatório) + campos a serem atualizados (mesma validação de e-mail).
- `intent: "tenant.delete"`:
  - `id` (string, obrigatório) — Retorna erro se o inquilino tiver contratos vinculados.

#### Imóveis (`properties`):
- `intent: "property.create"`:
  - `name` (string, obrigatório)
  - `address` (string, obrigatório)
  - `city` (string, obrigatório)
  - `state` (string, obrigatório)
  - `zip` (string, opcional)
- `intent: "property.update"`:
  - `id` (string, obrigatório) + campos a atualizar.
- `intent: "property.delete"`:
  - `id` (string, obrigatório) — Bloqueado se houver contratos vinculados.

#### Contratos (`leases`):
- `intent: "lease.create"`:
  - `tenant_id` (string, obrigatório)
  - `property_id` (string, obrigatório)
  - `start_date` (string `YYYY-MM-DD`, obrigatório)
  - `end_date` (string `YYYY-MM-DD`, obrigatório)
  - `due_day` (integer 1-31, obrigatório)
  - `rent_amount` (number, obrigatório)
- `intent: "lease.update"`:
  - `id` (string, obrigatório) + campos a atualizar.
- `intent: "lease.terminate"`:
  - `id` (string, obrigatório) — Altera o status para `'terminated'`.
- `intent: "lease.reactivate"`:
  - `id` (string, obrigatório) — Altera o status para `'active'`.

#### Recibos e Pagamentos (`receipts`):
- `intent: "receipt.create"`:
  - `lease_id` (string, opcional se for avulso, obrigatório para aluguel)
  - `tenant_id` (string, opcional para avulso)
  - `property_id` (string, opcional para avulso)
  - `category` (string: `'rent'` ou `'extra'`)
  - `description` (string, obrigatório se for `'extra'`)
  - `due_date` (string `YYYY-MM-DD`, obrigatório)
  - `amount` (number, obrigatório)
  - `paid_at` (string ISO ou data/hora local, opcional)
  - `paid_amount` (number, opcional para pagamento parcial ou total)
- `intent: "receipt.update"`:
  - `id` (string, obrigatório) + campos a atualizar.
- `intent: "receipt.generate-month"`:
  - `month` (string `MM`, obrigatório)
  - `year` (string `YYYY`, obrigatório)
  - Gera os recibos de todos os contratos ativos do mês especificado que ainda não possuam recibo gerado.
- `intent: "receipt.batch-pay"`:
  - `ids` (string delimitada por vírgula de IDs de recibos)
  - Registra a baixa imediata de todos os recibos informados.
- `intent: "receipt.delete"`:
  - `id` (string, obrigatório)

---

## 4. Configurações do Sistema

### `GET /settings`
Renderiza a página de configurações gerais do locador, URL pública e robô WhatsApp.

### `POST /settings`
Processa mutações de configuração conforme o campo `intent`:
- `intent: "save-landlord"`:
  - `name` (string, obrigatório)
  - `cpf` (string, opcional)
  - `email` (string, opcional): validado no formato `usuario@dominio.tld` quando informado.
- `intent: "save-app-url"`:
  - `app_url` (string, URL pública para geração de QR Codes)
- `intent: "change-password"`:
  - `current_password` (string, obrigatório)
  - `new_password` (string, mínimo 4 caracteres)
  - `confirm_password` (string, confirmação)

---

## 5. Backup e Restauração

### `GET /export`
- **Autenticação**: Requerida (Sessão).
- **Resposta**: `200 OK` (JSON) com cabeçalho `Content-Disposition: attachment; filename="aluxen-backup-YYYY-MM-DD.json"`.
- Exporta o dump consolidado de todas as tabelas do banco de dados SQLite.

### `POST /import`
- **Autenticação**: Requerida (Sessão).
- **Corpo**: `multipart/form-data` com campo `backup_file` (arquivo JSON).
- Restaura os registros em transação atômica preservando a integridade referencial.

---

## 6. Emissão de Documentos em PDF

### `GET /pdf`
Emite o Recibo de Pagamento de Aluguel formatado em folha A4.
- **Autenticação**: Requer sessão administrativa **ou** `token` válido **ou** sessão do locatário titular do recibo (Área do Cliente).
- **Query Params**:
  - `id` (string, obrigatório): ID do recibo.
  - `token` (string, opcional se logado): Token HMAC-SHA256 de autenticidade para acesso público.
  - `assinatura` (string, opcional): `'false'` para emitir sem assinatura digital.
- **Resposta**: `200 OK` com `Content-Type: application/pdf`.
- Sessão de locatário só acessa recibos do próprio cadastro e já quitados; documentos de outro titular respondem `403`.

### `GET /contract-pdf`
Emite o Contrato de Locação Residencial com 25 cláusulas legais completas.
- **Autenticação**: Requer sessão administrativa **ou** `token` válido **ou** sessão do locatário titular do contrato.
- **Query Params**:
  - `id` (string, obrigatório): ID do contrato.
  - `token` (string, opcional se logado): Token HMAC-SHA256 de autenticidade.
- **Resposta**: `200 OK` com `Content-Type: application/pdf`.
- Sessão de locatário só acessa contratos do próprio cadastro.

---

## 7. Portal Público de Validação

### `GET /validar/recibo/:id?token=...`
Valida publicamente a autenticidade de um recibo.
- **Autenticação**: Pública.
- **Resposta**:
  - `200 OK`: Documento autêntico, exibindo dados e botão de download do PDF original.
  - `400 Bad Request`: Token inválido, adulterado ou recibo inexistente.

### `GET /validar/contrato/:id?token=...`
Valida publicamente a autenticidade de um contrato de locação.
- **Autenticação**: Pública.
- **Resposta**:
  - `200 OK`: Contrato autêntico com vigência e qualificações das partes.
  - `400 Bad Request`: Token inválido ou contrato inexistente.

---

## 8. Integração e Automação do WhatsApp

Todas as rotas abaixo requerem sessão de usuário autenticado:

### `GET /api/whatsapp/status`
Retorna se o robô está conectado, se há QR Code pendente e se o Chromium está instalado.
```json
{
  "isLoggedIn": true,
  "hasQRCode": false,
  "chromiumAvailable": true,
  "profileDir": "/home/passoz/dev/aluxen/.whatsapp-profile"
}
```

### `POST /api/whatsapp/login/start`
Inicia a instância headless do Chromium para iniciar o pareamento do WhatsApp Web.

### `GET /api/whatsapp/login/status`
Consulta o estado da máquina de estados do login (`idle`, `starting`, `scan_qr`, `syncing`, `connected`, `error`).
Se `status === 'scan_qr'`, o campo `qrCode` trará o data URL da imagem do QR Code (`data:image/png;base64,...`).

### `POST /api/whatsapp/login/cancel`
Cancela a tentativa de login e fecha o navegador headless ativo.

### `POST /api/whatsapp/logout`
Desconecta a conta do WhatsApp Web e limpa a sessão salva do robô.

### `POST /api/whatsapp/preview`
Gera a mensagem formatada para WhatsApp e a URL direta (`wa.me`).
- **Corpo**:
  - `type`: `'receipt'` ou `'lease'`
  - `id`: ID da entidade
- **Resposta**:
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

### `POST /api/whatsapp/send`
Inicia o envio automático pelo WhatsApp em segundo plano e responde imediatamente com o identificador do acompanhamento.
- **Corpo**:
  - `to` (string, número de telefone do destinatário)
  - `message` (string, texto da mensagem a ser enviada)
- **Resposta**:
```json
{ "ok": true, "jobId": "0f5b1c2e-..." }
```

### `GET /api/whatsapp/send/status?jobId=...`
Consulta o andamento do envio iniciado em `POST /api/whatsapp/send`.
- **Etapas** (`job.stage`): `preparing`, `connecting`, `opening_chat`, `sending`, `confirming`, `done`
- **Status** (`job.status`): `running`, `done`, `error`
- **Resposta**:
```json
{
  "ok": true,
  "job": {
    "id": "0f5b1c2e-...",
    "status": "done",
    "stage": "done",
    "result": {
      "success": true,
      "to": "5511999998888",
      "message": "Olá, *João da Silva*!...",
      "timestamp": "2026-09-08T14:35:00.000Z"
    }
  }
}
```
Jobs expiram 5 minutos após a última atualização. Envios de outros usuários retornam `404`.

---

## 9. Histórico e Auditoria

### `GET /dashboard?tab=history`
Renderiza a aba **Histórico e Auditoria** no servidor, já com a primeira página da trilha (25 eventos mais recentes) embutida no HTML. Os filtros subsequentes são carregados via `GET /audit`.

### `GET /audit`
- **Autenticação**: Requerida (Sessão).
- **Resposta**: `200 OK` (JSON).

| Query Param | Padrão | Descrição |
|---|---|---|
| `page` | `1` | Página solicitada. |
| `pageSize` | `25` | Eventos por página (mínimo 5, máximo 200). |
| `order` | `desc` | Ordenação cronológica (`asc` ou `desc`). |
| `entity` | — | Filtra pela área (`tenant`, `property`, `lease`, `receipt`, `setting`, `user`, `document`, `whatsapp`, `backup`). |
| `action` | — | Filtra pela ação exata (ex.: `tenant.update`, `receipt.pay`, `document.receipt`). |
| `actor` | — | Filtra pelo nome do usuário que executou a ação. |
| `from` / `to` | — | Intervalo de datas (`YYYY-MM-DD`), inclusivo, no fuso do servidor. |
| `search` | — | Busca textual em resumo, registro, autor e identificador. |

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
        { "field": "paidAt", "label": "Pago em", "from": null, "to": "10/09/2026 09:42" },
        { "field": "paidAmount", "label": "Valor pago", "from": null, "to": "R$ 2.500,00" }
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
  "actors": ["passoz"]
}
```

`createdAt` usa o horário local do servidor (`YYYY-MM-DDTHH:mm:ss.SSS`), a mesma convenção dos demais horários do sistema. `changes` traz valores já formatados para leitura (moeda em BRL, datas em `dd/mm/aaaa`, situações traduzidas). `metadata` guarda contexto adicional do evento (competência gerada, lote, protocolo de envio, motivo de falha). Segredos nunca são gravados: senhas, tokens e sessões são omitidos ou redigidos.

### `GET /audit/export`
- **Autenticação**: Requerida (Sessão).
- **Query Params**: os mesmos filtros de `GET /audit`.
- **Resposta**: `200 OK` com `Content-Type: text/csv; charset=utf-8` e `Content-Disposition: attachment; filename="aluxen-historico-YYYY-MM-DD.csv"`.
- CSV com BOM UTF-8 e separador `;` (abre direto no Excel em português), com as colunas *Data/Hora, Usuário, Ação, Registro, Resumo, Alterações, Origem, IP, Request*. Limitado a 5.000 linhas por arquivo.
- A própria exportação é registrada na trilha (`history.export`).

---

## 10. Área do Cliente (Portal do Locatário)

Autenticação própria do locatário, independente do painel administrativo: cookie `__aluxen_client_session`, credencial por CPF e escrita inexistente (somente leitura de documentos próprios). Um mesmo navegador pode manter as duas sessões abertas sem interferência.

### `GET /portal/login`
Retorna a tela de login da Área do Cliente (SSR). Redireciona para `/portal` se já houver sessão de locatário.

### `POST /portal/login`
Autentica o locatário pelo CPF, com a mesma proteção de rate limit do painel (por IP).
- **Corpo**: `application/x-www-form-urlencoded` ou `multipart/form-data`
  - `cpf` (string, obrigatório): qualquer formatação (`529.982.247-25` ou `52998224725`).
  - `password` (string, obrigatório): senha pessoal; no primeiro acesso, o próprio CPF sem pontuação.
- **Respostas**:
  - `302 Found`: Sucesso. Define `__aluxen_client_session` e redireciona para `/portal/password` (senha padrão ainda vigente) ou `/portal`.
  - `400 Bad Request`: Campos ausentes.
  - `401 Unauthorized`: CPF ou senha inválidos (mensagem única, sem revelar se o CPF existe).
  - `429 Too Many Requests`: Limite de tentativas por minuto atingido.

### `POST /portal/logout` ou `GET /portal/logout`
Encerra a sessão do locatário e redireciona para `/portal/login`.

### `GET /portal`
Área do Cliente em modo leitura: contratos do titular (com vigência, vencimento, aluguel e links para PDF e autenticidade) e **apenas os recibos quitados** (integral ou parcial), com data e valor pago.
- **Autenticação**: Requerida (sessão de locatário).
- **Resposta**:
  - `302 Found`: Sem sessão → `/portal/login`; senha padrão ainda vigente → `/portal/password`.
  - `200 OK`: HTML renderizado com os dados do titular.

### `GET /portal/password`
Formulário de troca de senha. Indica quando a troca é obrigatória (primeiro acesso com a senha padrão).
- **Autenticação**: Requerida (sessão de locatário).

### `POST /portal/password`
Troca a senha do locatário autenticado.
- **Corpo**: `current_password`, `new_password`, `confirm_password` (todos obrigatórios).
- **Regras**: senha atual deve conferir (o CPF vale como senha atual no primeiro acesso); nova senha com no mínimo 6 caracteres, diferente do CPF e igual à confirmação.
- **Respostas**:
  - `302 Found`: Sucesso → `/portal?ok=password`.
  - `400 Bad Request`: Divergência de confirmação, senha atual incorreta ou nova senha inválida.
