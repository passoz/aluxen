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

#### Inquilinos (`tenants`):
- `intent: "tenant.create"`:
  - `name` (string, obrigatório)
  - `email` (string, obrigatório)
  - `phone` (string, opcional)
  - `cpf` (string, opcional)
- `intent: "tenant.update"`:
  - `id` (string, obrigatório) + campos a serem atualizados.
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
  - `email` (string, opcional)
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
- **Autenticação**: Requer sessão **ou** `token` válido.
- **Query Params**:
  - `id` (string, obrigatório): ID do recibo.
  - `token` (string, opcional se logado): Token HMAC-SHA256 de autenticidade para acesso público.
  - `assinatura` (string, opcional): `'false'` para emitir sem assinatura digital.
- **Resposta**: `200 OK` com `Content-Type: application/pdf`.

### `GET /contract-pdf`
Emite o Contrato de Locação Residencial com 25 cláusulas legais completas.
- **Autenticação**: Requer sessão **ou** `token` válido.
- **Query Params**:
  - `id` (string, obrigatório): ID do contrato.
  - `token` (string, opcional se logado): Token HMAC-SHA256 de autenticidade.
- **Resposta**: `200 OK` com `Content-Type: application/pdf`.

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
Dispara a mensagem em segundo plano utilizando o robô headless.
- **Corpo**:
  - `to` (string, número de telefone do destinatário)
  - `message` (string, texto da mensagem a ser enviada)
- **Resposta**:
```json
{
  "ok": true,
  "success": true,
  "to": "5511999998888",
  "message": "Mensagem enviada com sucesso!",
  "timestamp": "2026-09-08T14:35:00.000Z"
}
```
