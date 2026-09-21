# WhatsApp no Aluxen (Go) — modos demo e cliente

Este documento descreve o canal de WhatsApp do Aluxen no port Go. Ele substitui o
robô headless com Puppeteer/Chromium do app Bun (documento do app original, removido do repositório): no
Go **não há navegador** — o pareamento, o estado da sessão e o envio são chamadas
HTTP ao provedor do canal, e as mensagens recebidas entram por webhook.

O canal é **bidirecional**: o Aluxen envia recibos, contratos e cobranças, e
recebe as mensagens que o locatário manda. As mensagens recebidas ficam
persistidas e vinculadas ao inquilino, e alimentam o agente de atendimento com
ferramentas tipadas e limitadas ao titular da conversa.

---

## 1. Os dois modos — leia antes de configurar

O modo é escolhido por `WHATSAPP_MODE` e muda o risco e a configuração:

| | `WHATSAPP_MODE=demo` | `WHATSAPP_MODE=client` |
|---|---|---|
| Canal | **Não-oficial** (WAHA sobre WhatsApp Web) | **Cloud API oficial da Meta** |
| Termos da Meta | **Viola os termos** e pode **banir o número** | Permitido — canal oficial |
| Uso indicado | **Só demonstração** e avaliação | Cliente pagante, em produção |
| `WAHA_API_URL` | Obrigatório — aponta para o WAHA | Obrigatório — aponta para o canal oficial |
| `WAHA_WEBHOOK_SECRET` | Opcional (demo sobe sem segredo) | **Obrigatório** |

### 1.1. Modo demo (`WHATSAPP_MODE=demo`)

- Usa o **WAHA** — um canal **não-oficial**, que conversa com o WhatsApp Web.
- **Risco:** o uso viola os termos de serviço da Meta e **pode banir o número**
  pareado. Não use número de cliente; use um número descartável de demonstração.
- É o **padrão** (`WHATSAPP_MODE` sem valor cai em `demo`), para que a demo suba
  pronta sem exigir o canal oficial.
- Com `WAHA_WEBHOOK_SECRET` **vazio**, o webhook aceita qualquer remetente — o
  que só é tolerado **neste modo**. O handler registra um aviso no log a cada
  webhook aberto aceito. Com segredo configurado, o comportamento é o mesmo do
  modo cliente (fail-closed).

### 1.2. Modo cliente (`WHATSAPP_MODE=client`)

- Usa a **Cloud API oficial da Meta** — o canal permitido para operar com número
  de cliente.
- **`WAHA_WEBHOOK_SECRET` é obrigatório.** Sem assinatura HMAC válida no webhook,
  a mensagem recebida é recusada (`fail-closed`).
- É o modo recomendado para qualquer instância de cliente pagante.

### 1.3. Recomendação de uso

- **Demonstração:** `WHATSAPP_MODE=demo`, com um número descartável e ciência de
  que ele pode ser banido. O webhook pode ficar sem segredo para simplificar.
- **Cliente pagante:** `WHATSAPP_MODE=client`, canal oficial e
  `WAHA_WEBHOOK_SECRET` **sempre** configurado. Nunca aponte um número de cliente
  para o modo demo.

---

## 2. Configuração

```env
# demo (não-oficial, risco de banimento) ou client (Cloud API oficial)
WHATSAPP_MODE=demo

# Base da API do canal (WAHA no modo demo; canal oficial no modo cliente).
WAHA_API_URL=
# Chave que autentica as chamadas ao canal.
WAHA_API_KEY=
# Sessão usada por esta instância.
WAHA_SESSION=default

# Segredo HMAC que valida o webhook de mensagens recebidas.
# Obrigatório no modo client; no modo demo pode ficar vazio (webhook aberto).
WAHA_WEBHOOK_SECRET=
```

- Sem `WAHA_API_URL`, o canal fica **desligado**: a instância sobe normalmente e
  todo método do canal devolve "WAHA não configurado". O envio por `wa.me`
  continua funcionando.
- O campo `chromiumAvailable` de `GET /api/whatsapp/status` continua existindo por
  paridade com o app antigo, mas agora significa **"o canal está disponível"**
  (o provedor configurado responde), não "há Chromium instalado".

---

## 3. Pareamento (leitura do QR Code)

O pareamento é o mesmo fluxo do app antigo, agora por HTTP:

1. O painel chama `POST /api/whatsapp/login/start`.
2. O canal inicia a sessão e o WhatsApp gera o QR Code.
3. O painel consulta `GET /api/whatsapp/login/status` e exibe o QR Code (imagem
   em data URL PNG).
4. O locador lê o QR Code em **Aparelhos Conectados → Conectar um aparelho**.
5. A sessão passa por `starting` → `scan_qr` → `syncing` → `connected`.

O pareamento acompanha a sessão por até **3 minutos** (consulta a cada **1,5 s**),
os mesmos tempos do app antigo. `POST /api/whatsapp/login/cancel` interrompe um
pareamento em curso e `POST /api/whatsapp/logout` desconecta a sessão.

> **Hospedagem contínua:** o QR Code e os links de validação apontam para a URL
> pública configurada (`app_url`/`APP_URL`). Servidor desligado, QR morto — ver
> `docs/VALIDACAO-GO.md`.

---

## 4. Webhook de mensagens recebidas

- Rota: `POST /api/whatsapp/webhook` (isenta de CSRF; a autenticação é própria).
- Autenticação: assinatura **HMAC-SHA256** do corpo cru, no header
  `X-Webhook-Hmac` (o WAHA manda o hex do digest; também são aceitos base64 e o
  prefixo `sha256=`). A comparação é em tempo constante.
- **Fail-closed:** sem segredo configurado a assinatura é sempre recusada. A
  única exceção é **modo demo com segredo vazio** (ver seção 1.1).
- Eventos que não são mensagem (estado de sessão, confirmações de leitura,
  presença) são ignorados sem erro.

O que chega vira `domain.WhatsAppMessage`, persistido e vinculado ao inquilino
pelo telefone. O conteúdo da mensagem **não** é logado — o log guarda id,
telefone mascarado, estágio e status.

---

## 5. Endpoints do canal

Todas as rotas exigem sessão autenticada de administrador (exceto o webhook):

| Método | Rota | Descrição |
|---|---|---|
| `GET` | `/api/whatsapp/status` | Estado do canal (`isLoggedIn`, `hasQRCode`, `chromiumAvailable` = canal disponível). |
| `POST` | `/api/whatsapp/login/start` | Inicia o pareamento. |
| `GET` | `/api/whatsapp/login/status` | Polling do pareamento e QR Code em base64. |
| `POST` | `/api/whatsapp/login/cancel` | Cancela o pareamento em curso. |
| `POST` | `/api/whatsapp/logout` | Desconecta e descarta a sessão. |
| `POST` | `/api/whatsapp/preview` | Prévia do texto da mensagem e URL direta (`wa.me`). |
| `POST` | `/api/whatsapp/send` | Enfileira o envio em segundo plano e devolve o `jobId`. |
| `GET` | `/api/whatsapp/send/status` | Polling do envio (`preparing`, `connecting`, `opening_chat`, `sending`, `confirming`, `done`). |
| `POST` | `/api/whatsapp/webhook` | **Novo** — recebe as mensagens do locatário (valida HMAC). |

---

## 6. Envio e conversas

- O envio passa pela fila de trabalho (`internal/application/jobs.go`): a rota
  devolve um `jobId` e o painel acompanha por `GET /api/whatsapp/send/status`.
- As mensagens enviadas e recebidas aparecem em **Conversas**
  (`GET /api/conversations`); a resposta do locador é registrada por
  `POST /dashboard/conversation`.
- O agente de atendimento responde a partir de ferramentas **tipadas e limitadas
  ao titular da conversa** — nenhum acesso livre ao banco pelo modelo. Pergunta
  jurídica ou fora de escopo **escala para humano**. Os valores usados na cobrança
  vêm sempre do recibo, nunca do modelo.
- A régua de cobrança é disparada por `POST /api/collection/run`.
