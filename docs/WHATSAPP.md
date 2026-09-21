# Robô do WhatsApp — Guia Completo e Operação

> **Documento legado (app Bun).** Descreve o robô headless com Puppeteer/Chromium
> do app Bun, **substituído no branch `devgo`** pelo canal HTTP do port Go (WAHA).
> Para a versão vigente — modos demo/cliente, configuração e webhook — consulte
> [`WHATSAPP-GO.md`](WHATSAPP-GO.md). Mantido como referência histórica.

O **Aluxen** conta com um sistema integrado para envio de comprovantes de pagamento e contratos de locação via **WhatsApp**, operando tanto de forma **direta** (abrindo o WhatsApp Web ou aplicativo do usuário via link `wa.me`) quanto de forma **100% automatizada em segundo plano** (Robô Headless utilizando Puppeteer e Chromium).

---

## 1. Modos de Envio Disponíveis

### 1.1. Envio Direto (Link Oficial WhatsApp / `wa.me`)
- Gera uma URL segura no formato `https://wa.me/55DDNNNNNNNN?text=...`.
- Abre diretamente no WhatsApp Desktop, aplicativo mobile ou WhatsApp Web do navegador do locador.
- **Vantagem**: Não requer nenhuma dependência externa no servidor (funciona mesmo sem Chromium instalado).

### 1.2. Robô Headless (Envio Automático em Segundo Plano)
- Utiliza **Puppeteer Core** controlando uma instância headless do **Chromium**.
- O locador faz a leitura do QR Code **uma única vez**.
- A sessão fica salva permanentemente em disco (perfil do navegador).
- O robô abre a conversa com o locatário, preenche o texto formatado com os links de autenticação do PDF e dispara a mensagem sem interferência humana.

---

## 2. Máquina de Estados da Conexão

O ciclo de vida do login é orquestrado pela classe `WhatsAppLoginManager` (`src/whatsapp/login-manager.ts`):

```
     +-------------------------------------------------------------+
     |                                                             |
     v                                                             |
+----------+      /api/whatsapp/login/start       +--------------+ |
|   IDLE   | -----------------------------------> |   STARTING   | |
+----------+                                      +-------+------+ |
     ^                                                    |        |
     | logout / cancel                                    v        |
     |                                            +--------------+ |
     +------------------------------------------- |   SCAN_QR    | |
     |                                            +-------+------+ |
     |                                                    |        |
     |                                       QR Escaneado v        |
     |                                            +--------------+ |
     |                                            |   SYNCING    | |
     |                                            +-------+------+ |
     |                                                    |        |
     |                                      Painel Pronto v        |
     |                                            +--------------+ |
     +------------------------------------------- |  CONNECTED   | |
                                                  +--------------+ |
                                                          |        |
                                         Sessão Expirada / Erro    |
                                                          +--------+
```

### Estados:
- **`idle`**: Nenhuma tentativa de pareamento em andamento.
- **`starting`**: Lançando o binário do Chromium e abrindo a página do WhatsApp Web.
- **`scan_qr`**: O QR Code foi gerado na tela do WhatsApp Web, capturado e convertido em imagem base64 para exibição na UI ou no terminal.
- **`syncing`**: O usuário leu o QR Code no celular e o WhatsApp Web está sincronizando as mensagens.
- **`connected`**: WhatsApp Web conectado e pronto para despachar mensagens de forma headless.
- **`error` / `timeout`**: Falha ao abrir o navegador ou tempo limite de pareamento excedido.

---

## 3. Como Conectar o WhatsApp

Existem dois métodos para autenticar o robô:

### Método A: Diretamente pela Interface Web (Recomendado)
1. Acesse o sistema e entre em **Configurações** (`/settings`) ou abra o modal de envio no **Painel**.
2. Na seção **Robô do WhatsApp**, clique em **Conectar WhatsApp (Ler QR Code)**.
3. Um modal será exibido contendo o QR Code dinâmico com atualização em tempo real.
4. No seu smartphone, abra o WhatsApp, vá em **Aparelhos Conectados** > **Conectar um aparelho** e aponte a câmera para o QR Code da tela.
5. Assim que a sincronização for concluída, o modal se fechará e o status mudará para `🟢 WhatsApp Conectado e Ativo`.

### Método B: Pelo Terminal CLI
Se você estiver rodando em um servidor Linux sem interface gráfica ou preferir o terminal:

```bash
bun run whatsapp:login
```

O comando irá:
1. Localizar o binário do Chromium no sistema.
2. Iniciar o navegador em segundo plano.
3. Renderizar o QR Code em ASCII diretamente no seu terminal através de `qrcode-terminal`.
4. Aguardar a leitura do QR Code pelo celular.
5. Confirmar a autenticação e salvar o perfil da sessão.

---

## 4. Estrutura do Perfil e Persistência

A sessão do WhatsApp Web é salva na pasta de perfil do Chromium para que o login não seja perdido ao reiniciar a aplicação ou o container:

- **Local padrão**: `.whatsapp-profile/` na raiz do projeto.
- **Configurável via variável de ambiente**:
  ```env
  WHATSAPP_PROFILE_DIR=/data/.whatsapp-profile
  ```
- **Fallback para volume persistente**: Se `DB_DIR` estiver definido, o perfil será gravado em `$DB_DIR/.whatsapp-profile`.

> ⚠️ **Importante**: Em ambientes Docker, certifique-se de que a pasta de perfil esteja dentro de um **volume nomeado** ou **bind mount** persistente para evitar perder o pareamento nos restarts.

---

## 5. Formato das Mensagens Automáticas

As mensagens são pré-formatadas com campos estruturados e emojis amigáveis:

### 5.1. Mensagem de Recibo de Aluguel
```text
Olá, *João da Silva*! Tudo bem?

Segue o comprovante do seu aluguel referente ao imóvel *Apto 302*:

📄 *Documento*: Recibo de Pagamento de Aluguel
📅 *Vencimento*: 10/09/2026
💰 *Valor*: R$ 2.500,00
✅ *Status*: Quitado em 08/09/2026 às 14:30

Você pode visualizar, baixar o recibo oficial em PDF ou validar sua autenticidade nos links abaixo:

📥 *Baixar Recibo (PDF)*:
https://aluxen.evolucsia.com/pdf?id=01a0831e-...&token=6b957d...

🔍 *Verificar Autenticidade*:
https://aluxen.evolucsia.com/validar/recibo/01a0831e-...?token=6b957d...

Atenciosamente,
*Administração Aluxen*
```

### 5.2. Mensagem de Contrato de Locação
```text
Olá, *João da Silva*! Tudo bem?

Segue o Contrato de Locação Residencial referente ao imóvel *Apto 302*:

📄 *Documento*: Contrato de Locação Residencial
📅 *Vigência*: 01/10/2026 até 30/09/2027
💰 *Aluguel Mensal*: R$ 2.500,00 (Vencimento no dia 10)
📌 *Status*: Ativo

Você pode visualizar, baixar o contrato oficial em PDF ou conferir a validade jurídica nos links abaixo:

📥 *Baixar Contrato (PDF)*:
https://aluxen.evolucsia.com/contract-pdf?id=01a0831e-...&token=1a0d57...

🔍 *Verificar Autenticidade*:
https://aluxen.evolucsia.com/validar/contrato/01a0831e-...?token=1a0d57...

Atenciosamente,
*Administração Aluxen*
```

---

## 6. Endpoints da API do WhatsApp

Todas as rotas exigem sessão autenticada de usuário:

| Método | Rota | Descrição |
|---|---|---|
| `GET` | `/api/whatsapp/status` | Retorna o status atual (`isLoggedIn`, `hasQRCode`, `chromiumAvailable`, etc.). |
| `POST` | `/api/whatsapp/login/start` | Inicia o processo de pareamento do robô. |
| `GET` | `/api/whatsapp/login/status` | Polling do estado do pareamento e imagem do QR Code em base64. |
| `POST` | `/api/whatsapp/login/cancel` | Cancela o processo de pareamento ativo. |
| `POST` | `/api/whatsapp/logout` | Desconecta o WhatsApp e limpa a sessão do robô. |
| `POST` | `/api/whatsapp/preview` | Retorna o preview do texto da mensagem formatada e URL direta (`wa.me`). |
| `POST` | `/api/whatsapp/send` | Inicia o envio automático em segundo plano e retorna o `jobId` do acompanhamento. |
| `GET` | `/api/whatsapp/send/status` | Polling do andamento do envio (`preparing`, `connecting`, `opening_chat`, `sending`, `confirming`, `done`). |

---

## 7. Configuração do Chromium e Resolução de Problemas

### 7.1. Detecção do Binário do Chromium
O sistema busca o executável do Chromium automaticamente nos seguintes caminhos padrão do sistema operacional:
- `/usr/bin/chromium`
- `/usr/bin/chromium-browser`
- `/usr/bin/google-chrome`
- `/usr/bin/google-chrome-stable`
- `/snap/bin/chromium`
- `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome` (macOS)
- `C:\Program Files\Google\Chrome\Application\chrome.exe` (Windows)

Caso o Chromium esteja instalado em um caminho customizado, defina:
```bash
export CHROMIUM_PATH=/meu/caminho/para/chromium
```

### 7.2. Execução dentro do Docker
No Dockerfile oficial do Aluxen, o Chromium e as fontes necessárias já são instalados:
```dockerfile
RUN apt-get update && apt-get install -y --no-install-recommends \
    chromium \
    chromium-sandbox \
    fonts-freefont-ttf \
    fonts-liberation \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*
ENV CHROMIUM_PATH=/usr/bin/chromium
```

O Puppeteer é lançado automaticamente com os argumentos:
- `--no-sandbox`
- `--disable-setuid-sandbox`
- `--disable-dev-shm-usage` (previne travamentos de memória compartilhada em containers)
- `--disable-gpu`

### 7.3. Troubleshooting Frequente

1. **Erro: "Nenhum binário do Chromium ou Google Chrome foi encontrado"**:
   - No Debian/Ubuntu: `sudo apt install -y chromium chromium-sandbox fonts-liberation`
   - Defina `CHROMIUM_PATH=/usr/bin/chromium` no seu `.env`.

2. **Sessão desconectando com frequência**:
   - Certifique-se de que o aparelho celular não está com economia agressiva de bateria desativando o WhatsApp em segundo plano.
   - Verifique se a pasta `.whatsapp-profile/` possui permissões de leitura e escrita para o usuário que executa a aplicação.

3. **Mensagem não enviada / timeout de 45 segundos**:
   - O número de telefone deve conter o DDD (ex.: `11999998888` ou `5511999998888`). O Aluxen sanitiza e formata o DDI brasileiro `55` automaticamente.
