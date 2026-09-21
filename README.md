# Aluxen

Sistema completo de **gestão de locações imobiliárias**, escrito em **Go**: um **binário único** (imagem **distroless**, sem runtime de linguagem nem shell), com **SQLite** ou **Postgres** escolhidos por `DATABASE_URL`, **migrações automáticas no boot**, geração de **PDFs com autenticidade por QR Code** e canal de **WhatsApp** (WAHA / Cloud API) para envio e atendimento bidirecional.

> **Nota de branch (`devgo`):** este branch **substitui o app Bun** (TypeScript, Hono, Drizzle, React SSR, robô headless) pelo **port Go**. O app vigente é o binário em `cmd/app` sobre `internal/`; os diretórios `src/`, `public/`, `drizzle/` e `test/` pertencem ao **app Bun legado** e são mantidos apenas como referência histórica. A arquitetura vigente está em [`docs/ARQUITETURA-GO.md`](docs/ARQUITETURA-GO.md).

---

## 📑 Sumário

- [Visão Geral](#-visão-geral)
- [Stack Tecnológica](#-stack-tecnológica)
- [Funcionalidades Principais](#-funcionalidades-principais)
  - [1. Autenticação & Segurança](#1-autenticação--segurança)
  - [2. Gestão de Inquilinos](#2-gestão-de-inquilinos)
  - [3. Gestão de Imóveis](#3-gestão-de-imóveis)
  - [4. Contratos de Locação & Emissão Formal](#4-contratos-de-locação--emissão-formal)
  - [5. Pagamentos, Recibos & Baixa em Lote](#5-pagamentos-recibos--baixa-em-lote)
  - [6. Documentos em PDF com Autenticidade Digital](#6-documentos-em-pdf-com-autenticidade-digital)
  - [7. Portal Público de Validação de Documentos](#7-portal-público-de-validação-de-documentos)
  - [8. Integração de WhatsApp](#8-integração-de-whatsapp)
  - [9. Configurações do Locador & URL Pública](#9-configurações-do-locador--url-pública)
  - [10. Backup & Restauração JSON](#10-backup--restauração-json)
  - [11. Histórico & Auditoria](#11-histórico--auditoria)
  - [12. PWA, Responsividade & Sincronização Offline](#12-pwa-responsividade--sincronização-offline)
  - [13. Observabilidade & Logs Estruturados](#13-observabilidade--logs-estruturados)
  - [14. Área do Cliente (Portal do Locatário)](#14-área-do-cliente-portal-do-locatário)
  - [15. Informações do Sistema & Build em Execução](#15-informações-do-sistema--build-em-execução)
  - [16. Interface, Componentes & Usabilidade](#16-interface-componentes--usabilidade)
- [Guia Rápido de Instalação e Execução](#-guia-rápido-de-instalação-e-execução)
- [Variáveis de Ambiente](#-variáveis-de-ambiente)
- [Scripts Disponíveis](#-scripts-disponíveis)
- [Banco de Dados & Migrações](#-banco-de-dados--migrações)
- [Execução com Docker e Docker Compose](#-execução-com-docker-e-docker-compose)
- [Testes e Qualidade de Código](#-testes-e-qualidade-de-código)
- [Estrutura do Projeto](#-estrutura-do-projeto)
- [Documentação Técnica Aprofundada](#-documentação-técnica-aprofundada)

---

## 🌟 Visão Geral

O **Aluxen** foi desenvolvido para locadores e administradores de imóveis que buscam autonomia, agilidade e segurança jurídica. O sistema elimina dependências pesadas: com um **binário único** (sem runtime de linguagem) e um arquivo **SQLite** — ou um **Postgres** já existente —, você tem em mãos uma aplicação completa para cadastrar inquilinos, controlar contratos, emitir cobranças, gerar recibos em PDF com validação criptográfica e notificar locatários pelo WhatsApp.

---

## 🛠 Stack Tecnológica

| Componente | Tecnologia | Finalidade |
|---|---|---|
| **Runtime** | [Go](https://go.dev/) 1.27 | **Binário único** e estático (`CGO_ENABLED=0`); imagem final **distroless** (`gcr.io/distroless/static-debian12`), sem runtime de linguagem nem shell. |
| **HTTP** | `net/http` (ServeMux do Go 1.22+) | Roteamento por método+path, middlewares de sessão, CSRF e rate limit, e serving estático embutido (`embed`). |
| **Templates** | `html/template` | Renderização server-side das telas do painel, portal e validação, com escape automático. |
| **Banco de Dados** | `modernc.org/sqlite` **ou** `jackc/pgx/v5` | Um dialeto por instância, escolhido por `DATABASE_URL` (`file:...` → SQLite; `postgres://...` → Postgres). |
| **Migrações** | [`pressly/goose`](https://github.com/pressly/goose) | Migrações versionadas aplicadas **automaticamente no boot**, por dialeto. |
| **Geração de PDF** | [`go-pdf/fpdf`](https://github.com/go-pdf/fpdf) | Recibos, contratos, autos de vistoria e recibos de caução montados como modelo tipado e desenhados em A4. |
| **QR Code** | [`skip2/go-qrcode`](https://github.com/skip2/go-qrcode) | QR Codes de validação dos documentos e do pareamento do WhatsApp. |
| **Criptografia** | `crypto/hmac`, `crypto/sha256`, `golang.org/x/crypto/bcrypt` | Hash de senhas (bcrypt), tokens de autenticidade HMAC-SHA256 e comparação em tempo constante. |
| **Canal WhatsApp** | WAHA (demo) ou Cloud API oficial (cliente) | Envio de recibos/contratos/cobranças e **recebimento** de mensagens por webhook. |

---

## 🎯 Funcionalidades Principais

### 1. Autenticação & Segurança
- **Sessões HTTP-only Seguras**: Armazenadas em cookie criptografado (`__aluxen_session`), com flags `HttpOnly`, `SameSite=Lax`, e validade de 30 dias.
- **Duas autenticações independentes**: o painel do locador (`__aluxen_session`) e a **Área do Cliente** do locatário (`__aluxen_client_session`) usam cookies distintos, sem privilégios cruzados — a sessão do locatário não abre nenhuma rota administrativa.
- **Proteção contra Força Bruta**: três camadas em memória — limite por IP/minuto (`RATE_LIMIT_MAX`, padrão 5), teto global por minuto (`RATE_LIMIT_GLOBAL_MAX`, padrão 60, que barra varredura com IPs rotativos) e **bloqueio progressivo** do IP após falhas repetidas (`LOGIN_LOCK_THRESHOLD`, padrão 10 falhas → 5 min, dobrando até 1 h). Tentativas bloqueadas respondem `429` com `Retry-After` e ficam registradas na trilha com o usuário tentado.
- **Hashes Criptográficos**: Senhas protegidas com `bcryptjs` (salt cost 10).
- **Usuário Administrador Automático**: Semeado no boot caso nenhum usuário exista (`passoz` / `senha-de-teste`, customizável por `SEED_USERNAME`/`SEED_PASSWORD`). Enquanto a senha semeada não for trocada, o painel inteiro fica bloqueado até a definição de uma senha pessoal (mesma regra da Área do Cliente).
- **Troca obrigatória da senha padrão**: `POST /settings` grava a data da troca (`users.password_changed_at`); enquanto ela for nula, qualquer rota administrativa redireciona para a tela de troca (APIs respondem `403`). A Área do Cliente e os links públicos com token continuam acessíveis.

### 2. Gestão de Inquilinos
- Cadastro completo de locatários: Nome, CPF formatado, Email e Telefone/WhatsApp com DDD.
- Busca em tempo real e ordenação multi-coluna com paginação.
- Modais rápidos de cadastro e edição.
- **E-mail validado** no formulário (padrão `usuario@dominio.tld`) e também no servidor, garantindo o endereço usado nos envios de WhatsApp/e-mail.
- **Coluna "Área do Cliente"**: mostra quem já definiu senha própria (`Senha própria`), quem ainda usa a senha padrão (`Senha padrão`) e quem não tem CPF cadastrado (`Sem CPF`, portanto sem acesso ao portal).
- **Exclusão Segura**: O sistema impede a exclusão acidental de locatários que possuam contratos ativos.

### 3. Gestão de Imóveis
- Controle de propriedades residenciais e comerciais (Nome, Endereço, Cidade, Estado/UF, CEP).
- **CEP como primeiro campo**: com a máscara `00000-000`, a consulta ao ViaCEP preenche automaticamente endereço, cidade e estado — número e complemento ficam por conta do usuário. Se a consulta falhar, o cadastro manual continua disponível.
- Listagem com filtros, ordenação e contadores de ocupação.
- Proteção de integridade referencial ao excluir imóveis com contratos em vigência.

### 4. Contratos de Locação & Emissão Formal
- Vínculo direto entre Inquilino e Imóvel com definição de data inicial, data final, dia de vencimento mensal (`due_day`) e valor do aluguel.
- Controle de ciclo de vida do contrato (**Ativo** ou **Encerrado**), com botões para Encerrar ou Reativar.
- **Emissão Formal do Contrato em PDF (`/contract-pdf?id=...`)**:
  - Minuta completa com **25 cláusulas legais** baseadas na **Lei do Inquilinato (Lei nº 8.245/1991)**.
  - Qualificação integral de locador e locatário, garantias, foro de eleição e campos para assinaturas das partes e testemunhas.
  - **QR Code de autenticidade e validação pública** em todas as páginas.

### 5. Pagamentos, Recibos & Baixa em Lote
- **Geração Mensal em Lote**: Botão que gera automaticamente os recibos de todos os contratos ativos do mês selecionado, com chave única (`generated_key`) que impede duplicidades.
- **Recibos Avulsos / Extras**: Suporte a cobranças avulsas (IPTU, reparos, taxa de condomínio) desvinculadas de contrato obrigatório (`category = 'extra'`).
- **Valor sugerido pelo contrato**: ao escolher o contrato em um lançamento de aluguel, o campo de valor já vem preenchido com o aluguel vigente (editável).
- **Suporte a Pagamento Parcial**: Registro de `amount` (valor total) e `paid_amount` (valor efetivamente pago), permitindo quitações parciais.
- **Status Inteligentes**:
  - `Pago`: Quitado integralmente.
  - `Parcial`: Valor pago inferior ao valor total.
  - `Vencido`: Data de vencimento ultrapassada sem quitação.
  - `A vencer`: Aguardando pagamento.
- **Baixa em Lote (`receipt.batch-pay`)**: Seleção múltipla de recibos na tabela para liquidação simultânea em um único clique.

### 6. Documentos em PDF com Autenticidade Digital
- **Recibo de Pagamento (`/pdf?id=...`)**:
  - Valores monetários formatados e expressos por extenso em português (ex.: *"dois mil e quinhentos reais"*).
  - **Assinatura visualizada do locador**: a imagem da assinatura é impressa no documento; **não** é assinatura eletrônica com validade jurídica nos termos da ICP-Brasil (configurável via `&assinatura=false` para recibo em branco).
  - Data por extenso e declaração formal de quitação.
  - **Código de Autenticação Alfanumérico** (ex.: `AUTH-4061-AE09-3241-A94F`).
  - **QR Code Dinâmico**: Permite ao locatário escanear a folha do recibo com a câmera do celular e conferir os dados originais no servidor.

### 7. Portal Público de Validação de Documentos
- Acesso público e independente em:
  - **Recibos**: `/validar/recibo/:id?token=...`
  - **Contratos**: `/validar/contrato/:id?token=...`
- **Validação Criptográfica HMAC-SHA256**: Garante que o documento foi genuinamente gerado pelo sistema e não foi adulterado.
- **Visualização Segura**: Exibe o selo verde de autenticidade, valores, datas e um botão direto para **Visualizar / Baixar o PDF original**, dispensando a necessidade de o locatário criar login no sistema.
- **Auto-Detecção de IP de Rede Local**: Em modo de desenvolvimento, se você escanear o QR Code pelo smartphone na mesma rede Wi-Fi, o sistema resolve o IP local (ex.: `http://192.168.10.109:3000`) para que a página abra instantaneamente no celular!

### 8. Integração de WhatsApp
- **Envio Direto (`wa.me`)**: gera link oficial para envio manual pelo aplicativo ou WhatsApp Web, sem depender de canal configurado.
- **Canal HTTP (sem navegador)**: o pareamento, o estado da sessão e o envio são chamadas HTTP ao provedor do canal — **não há Chromium nem robô headless** no port Go.
  - **Modo demo** (`WHATSAPP_MODE=demo`): canal **não-oficial** (WAHA). Viola os termos da Meta e **pode banir o número** — só para demonstração.
  - **Modo cliente** (`WHATSAPP_MODE=client`): **Cloud API oficial da Meta**, com `WAHA_WEBHOOK_SECRET` **obrigatório**. Recomendado para cliente pagante.
  - **Pareamento por QR Code** no painel (`/settings`) ou no modal de envio.
  - **Bidirecional**: as mensagens recebidas entram por webhook (`POST /api/whatsapp/webhook`, com assinatura HMAC) e ficam persistidas e vinculadas ao inquilino.
  - Mensagens pré-formatadas com emojis, resumo financeiro, link para download do PDF e link do portal de validação.
- Detalhes em [`docs/WHATSAPP-GO.md`](docs/WHATSAPP-GO.md).

### 9. Configurações do Locador & URL Pública
- Cadastro de Nome, CPF e Email do Locador para emissão automática nos cabeçalhos de contratos e recibos.
- Configuração do Domínio / URL Pública do Sistema (`app_url`), permitindo apontar para o domínio de produção oficial (`https://aluxen.evolucsia.com`) ou domínio próprio.
- Painel de status e reconexão do canal de WhatsApp em tempo real.
- Alteração segura de senha do usuário autenticado.

### 10. Backup & Restauração JSON
- **Exportação Completa (`/export`)**: Gera download imediato de arquivo `aluxen-backup-YYYY-MM-DD.json` contendo todas as tabelas (inquilinos, imóveis, contratos, recibos e configurações).
- **Restauração / Importação (`/import`)**: Upload do arquivo JSON para restaurar ou migrar a base inteira em uma transação atômica.

### 11. Histórico & Auditoria
- **Trilha de auditoria completa (`audit_logs`)**: toda operação relevante é registrada com autor, data/hora, IP, protocolo da requisição e o **diff campo a campo** (valor antes → valor depois) já formatado em português.
- Cobre cadastros e alterações de inquilinos e imóveis, criação/alteração/encerramento de contratos, lançamentos, baixas (individuais e em lote), geração mensal, alteração de valores e de dados do locador, troca de senha, **emissões de PDF** (recibo e contrato), envios pelo WhatsApp, backups e acessos (login, tentativas negadas e saída).
- **Aba "Histórico" no painel**: filtros por área, ação, usuário e período, busca textual, ordenação cronológica, paginação, detalhe do evento com tabela *antes/depois* e **exportação CSV** (compatível com Excel em português).
- **Exportação CSV completa**: data/hora, usuário, ação, área, registro, ID, resumo, alterações, metadados, origem, IP, **user-agent** e protocolo da requisição — o suficiente para investigar incidentes fora da aplicação.
- Senhas, tokens e cookies **nunca** são gravados na trilha.

### 12. PWA, Responsividade & Sincronização Offline
- Suporte a Progressive Web App com manifesto (`/manifest.webmanifest`) e ícones em alta resolução para instalação no celular (iOS e Android).
- **Fila de Sincronização Offline**: Ações realizadas sem conexão à internet são retidas em `localStorage` e reenviadas ao servidor automaticamente assim que a conectividade for restabelecida.
- Sidebar retrátil com persistência no navegador e Drawer mobile para operação confortável em telas pequenas.
- Seletor de tema **Claro / Escuro** integrado.

### 13. Observabilidade & Logs Estruturados
- **Logger estruturado** (`internal/infrastructure/logging`): uma linha por evento com timestamp ISO, nível (`debug`, `info`, `warn`, `error`), escopo (`http`, `audit`, `db`, `whatsapp`, `app`) e campos tipados.
- **Dois formatos**: legível com cores de leitura no terminal (`pretty`, padrão em TTY) e **JSON Lines** para containers/observabilidade (`LOG_FORMAT=json`, padrão fora de TTY).
- **Log de requisições**: método, rota, status, duração em ms, IP, usuário autenticado e `requestId` — o mesmo protocolo aparece na resposta de erro e na trilha de auditoria. Estáticos vão para `debug`; 4xx para `warn`; 5xx e requisições acima de 1 s para `error`/`warn`.
- **Tratamento de erros**: exceções não tratadas são logadas com rastreio de pilha e devolvem uma página/JSON amigável com o protocolo de correlação (em vez de "Internal Server Error" genérico).
- **Redação automática** de campos sensíveis (`password`, `token`, `secret`, `cookie`, `hash`, `session`) em qualquer log.

### 14. Área do Cliente (Portal do Locatário)
- **Login próprio e separado do painel**: `/portal/login`, com link cruzado entre as duas telas. O locatário acessa apenas os próprios documentos; nenhuma rota de escrita é exposta.
- **Credencial inicial por CPF**: usuário e senha são o CPF sem pontuação (`529.982.247-25` → `52998224725`). A busca aceita o CPF formatado ou só dígitos.
- **Troca obrigatória no primeiro acesso**: enquanto a senha padrão estiver vigente, o portal bloqueia a leitura (`/portal` → `/portal/password`). Depois da troca, a senha pessoal (mínimo 6 caracteres, diferente do CPF) passa a valer e o CPF deixa de autenticar.
- **Leitura de contrato e recibos quitados**: contratos do titular com vigência, dia de vencimento e valor do aluguel; recibos **apenas os já quitados** (integrais ou parciais), com data e valor pago, PDF e link de autenticidade.
- **Isolamento por titular**: recibos em aberto e documentos de outros locatários não aparecem na listagem nem podem ser baixados via `/pdf` e `/contract-pdf` (`403`).
- **Auditoria integrada**: acessos, tentativas negadas, saídas e trocas de senha do locatário são registrados na trilha com a origem `client` (Área do Cliente).

### 15. Informações do Sistema & Build em Execução
- **Botão "Informações"** no rodapé da barra lateral (abaixo de *Configurações*), abrindo um modal com a identificação exata do que está rodando.
- **Versão que sempre avança**: a versão exibida é `MAJOR.MINOR` declarada em `internal/infrastructure/buildinfo` com o **número do build** como subversão (ex.: `1.0.61`) — o número vem da contagem de commits no pipeline (`git rev-list --count HEAD`) e é gravado na imagem, então cada build publicado tem uma versão diferente da anterior.
- **Identificação do build**: origem (`Imagem publicada (GHCR)` ou `Execução local`), versão, número do build, commit, branch/tag, **tag da imagem** (`ghcr.io/passoz/aluxen:sha-<commit>`), data de construção (UTC) e link direto para a execução do GitHub Actions que gerou a imagem.
- **Estado do sistema**: runtime do Go, ambiente, fuso horário, hora de início e tempo no ar, caminho e tamanho do banco, URL pública configurada e contagem de inquilinos, imóveis, contratos e recibos.
- Os metadados são injetados na imagem pelo workflow de publicação (`build-args` → `ENV` no `Dockerfile`) e lidos em runtime por `internal/infrastructure/buildinfo`; fora da imagem, o sistema cai para o repositório git local. Sem metadados, o modal avisa que a execução não é identificável.

### 16. Interface, Componentes & Usabilidade
- **Seletores e calendários com abertura inteligente**: os popovers abrem abaixo do campo; sem espaço, abrem acima e, quando não há altura suficiente em nenhuma direção, ao lado — sempre limitados pela janela e sem empurrar a página. São renderizados em portal, então não ficam recortados dentro de modais com rolagem.
- **Calendário destaca o dia corrente** (contorno na cor primária), independente da data selecionada, e traz os atalhos *Hoje*/*Agora* e *Limpar*.
- **Modais com rolagem interna**: modais altos (como o de Informações) rolam dentro do próprio cartão, limitado à altura da janela — a página atrás não ganha rolagem.
- **Filtros rotulados e alinhados em todas as seções**: busca, mês, ano, status, área, ação e usuário aparecem com rótulo acima do controle; rótulos e controles compartilham a mesma linha de base e a mesma altura (38px), com os campos empilhados na vertical em telas estreitas.
- **Seleção sem opção duplicada**: os selects com busca exibem uma única opção de placeholder, fornecida pela tela.
- **Formulários com validação nativa**: e-mail com `pattern` compartilhado entre cliente e servidor; a validação também roda no fluxo offline (`form.reportValidity()`).

---

## 🚀 Guia Rápido de Instalação e Execução

### Pré-requisitos
- [Go](https://go.dev/) 1.27 ou superior.
- **Nada mais**: não há Chromium, Bun nem runtime de linguagem na imagem de produção.

### 1. Clonar o repositório
```bash
git clone https://github.com/passoz/aluxen.git
cd aluxen
```

### 2. Baixar as dependências
```bash
go mod download
```

### 3. Executar em modo desenvolvimento
```bash
go run ./cmd/app
```
O servidor estará acessível em:
- **Navegador local**: `http://localhost:3000`
- **Pelo smartphone no mesmo Wi-Fi**: `http://<SEU-IP-LOCAL>:3000` (o sistema resolve o IP local para os QR Codes de validação)

As variáveis de ambiente são lidas do **próprio ambiente** (12-factor); o app Go **não** lê um arquivo `.env` automaticamente. Use `.env.example` como referência e exporte as chaves (ou use o container, que já recebe as variáveis prontas).

### 4. Compilar o binário
```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o aluxen ./cmd/app
```

### 5. Executar em produção
```bash
./aluxen
```
No boot, o app **abre o banco e aplica as migrações automaticamente**, semeia o administrador (se a base estiver vazia) e só então começa a servir. A prontidão é observável por `GET /healthz` (processo vivo) e `GET /readyz` (banco pronto).

### Credenciais Padrão no Primeiro Acesso
- **Painel administrativo**: usuário `passoz` / senha `senha-de-teste` — a troca é exigida no primeiro login (as variáveis `SEED_USERNAME`/`SEED_PASSWORD` valem apenas até essa troca).
- **Área do Cliente (locatário)** — `/portal/login`: usuário e senha são o **CPF do locatário sem pontuação** (ex.: CPF `529.982.247-25` → login `52998224725` e senha `52998224725`). A troca de senha é exigida no primeiro acesso. Locatários sem CPF cadastrado não conseguem entrar — complete o cadastro em *Inquilinos*.

---

## ⚙️ Variáveis de Ambiente

A configuração vem **do ambiente** (12-factor); `.env.example` é o contrato das chaves exigidas. O app Go **não** lê um `.env` automaticamente — exporte as variáveis ou deixe o container provê-las.

| Variável | Padrão | Descrição |
|---|---|---|
| `APP_ENV` | `development` | Ambiente de execução (`development`, `test` ou `production`). Em `production`, `SESSION_SECRET` é obrigatório (≥ 16 caracteres). |
| `HOST` | `0.0.0.0` | Endereço de bind de rede. |
| `PORT` | `3000` | Porta TCP do servidor HTTP. |
| `DATABASE_URL` | *(vazio)* | Dialeto do banco: `file:/data/aluxen.db` → **SQLite**; `postgres://user:pass@host:5432/db` → **Postgres**. Vazio usa `DB_DIR`. |
| `DB_DIR` | *(raiz)* | Diretório onde o arquivo `aluxen.db` é gravado quando `DATABASE_URL` está vazia (e base do `STORAGE_DIR`). |
| `STORAGE_DIR` | `<DB_DIR>/storage` | Diretório dos anexos (fotos de vistoria e documentos enviados). |
| `SESSION_SECRET` | *(obrigatório em produção)* | Assina sessão, CSRF e os tokens HMAC de autenticidade dos documentos. Sem valor seguro, o boot falha em produção. |
| `COOKIE_SECURE` | `false` | Força o atributo `Secure` dos cookies fora de produção (em produção é sempre `Secure`). |
| `APP_URL` | *(auto-detectado)* | URL pública base dos links e QR Codes (ex.: `https://aluxen.evolucsia.com`). Vence `BASE_URL`. |
| `BASE_URL` | *(vazio)* | URL pública usada quando `APP_URL` não é informado. |
| `SEED_USERNAME` | `passoz` | Usuário administrador criado no primeiro boot. |
| `SEED_PASSWORD` | `senha-de-teste` | Senha inicial do administrador. |
| `SEED_DEMO` | `false` | Popula a base com os dados de demonstração no boot (apenas para demo). |
| `DEFAULT_LANDLORD_NAME` | `Locador` | Nome padrão do locador nas configurações iniciais. |
| `DEFAULT_LANDLORD_CPF` | `""` | CPF inicial do locador. |
| `RATE_LIMIT_MAX` | `5` | Tentativas de login por IP na janela de 60 s. |
| `RATE_LIMIT_GLOBAL_MAX` | `60` | Teto global de tentativas de login na janela de 60 s. |
| `LOGIN_LOCK_THRESHOLD` | `10` | Falhas que iniciam o bloqueio progressivo do IP (5 min, dobrando até 1 h). |
| `LOG_LEVEL` | `info` | Nível mínimo dos logs: `debug`, `info`, `warn`, `error` ou `silent`. |
| `LOG_FORMAT` | `json` em produção, `pretty` em TTY | Formato do logger. |
| `WHATSAPP_MODE` | `demo` | `demo` (WAHA, não-oficial, risco de banimento) ou `client` (Cloud API oficial). Ver `docs/WHATSAPP-GO.md`. |
| `WAHA_API_URL` | *(vazio)* | Base da API do canal. Vazio desliga o canal (a instância sobe sem WhatsApp). |
| `WAHA_API_KEY` | *(vazio)* | Autentica as chamadas ao canal. |
| `WAHA_SESSION` | `default` | Sessão do canal usada pela instância. |
| `WAHA_WEBHOOK_SECRET` | *(vazio)* | Segredo HMAC que valida o webhook de mensagens recebidas. **Obrigatório** fora do modo demo. |
| `AI_API_URL` | *(vazio)* | Base da API do provedor de IA (leitura de documentos). |
| `AI_API_KEY` | *(vazio)* | Autentica as chamadas ao provedor de IA. |
| `AI_MODEL` | `deepseek-v4.1-flash` | Modelo usado nas extrações por IA (o operador de IA no aviso de privacidade). |
| `AI_CONFIDENCE_THRESHOLD` | `0.9` | Confiança mínima para a baixa automática; abaixo disso vai para revisão humana. |
| `EASYPANEL_URL` | *(vazio)* | Base da API de provisionamento por instância. |
| `EASYPANEL_API_KEY` | *(vazio)* | Autentica as chamadas à API de provisionamento. |
| `TZ` | `America/Sao_Paulo` (imagem) | Fuso do processo; define os horários da trilha de auditoria e dos logs. |
| `ALUXEN_BUILD_COMMIT`, `ALUXEN_BUILD_COMMIT_SHORT`, `ALUXEN_BUILD_REF`, `ALUXEN_BUILD_TIME`, `ALUXEN_BUILD_IMAGE`, `ALUXEN_BUILD_RUN_URL`, `ALUXEN_BUILD_NUMBER` | *(injetados na imagem)* | Metadados do build exibidos no modal **Informações**. `ALUXEN_BUILD_NUMBER` é a subversão da versão (`1.0.<número>`). Preenchidos pelo workflow de publicação via `build-args`; podem ser definidos manualmente para identificar uma execução local. |

---

## 📜 Comandos Disponíveis

O app Go se constrói e roda com a toolchain padrão (não há scripts de runtime):

```bash
# Executar em desenvolvimento
go run ./cmd/app

# Compilar o binário estático de produção
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o aluxen ./cmd/app

# Executar a suíte de testes
go test ./...

# Verificação estática (vet)
go vet ./...

# Provisionar uma instância no Easypanel (uma instância por cliente/demo)
go run ./cmd/provision
```

---

## 🗄 Banco de Dados & Migrações

O dialeto é escolhido no boot por `DATABASE_URL`: `file:...` usa **SQLite** (`modernc.org/sqlite`, modo WAL, chaves estrangeiras ativadas) e `postgres://...` usa **Postgres** (`jackc/pgx/v5`).

- Os arquivos de migração residem em `migrations/sqlite/` e `migrations/postgres/` — **os dois dialetos, com paridade de esquema**.
- As migrações são aplicadas por [`pressly/goose`](https://github.com/pressly/goose) **automaticamente no boot** (`db.Open`), no dialeto resolvido. Não há passo manual de migração.

---

## 🐳 Execução com Docker e Docker Compose

A imagem de produção é **multi-stage** e termina em **distroless** (`gcr.io/distroless/static-debian12`): só o binário Go, **sem Chromium, sem runtime de linguagem e sem shell**. O canal de WhatsApp é HTTP (WAHA/Cloud API), então não há navegador no container.

### Subir com Docker Compose:
```bash
docker compose up -d --build
```
O serviço estará acessível em `http://localhost:3000`, e os dados (banco e anexos) são persistidos no volume montado em `/data` (`DB_DIR=/data`).

### Imagem Oficial
A imagem de produção é publicada automaticamente via GitHub Actions no **GHCR**:
```bash
docker pull ghcr.io/passoz/aluxen:latest
docker run -d -p 3000:3000 -v /minha-pasta/data:/data ghcr.io/passoz/aluxen:latest
```

Cada build é publicado com duas tags: `latest` e `sha-<commit>`. O commit, a branch, a data de construção, a **versão (`1.0.<nº do build>`)** e a tag exata da imagem ficam gravados dentro da própria imagem (`build-args` → `ENV`) e aparecem no painel em **Informações** — dá para confirmar qual build está rodando sem consultar o GitHub. O deploy por instância está descrito em [`ARQUITETURA-DEPLOY.md`](ARQUITETURA-DEPLOY.md).

---

## 🧪 Testes e Qualidade de Código

A aplicação tem testes unitários e de integração em Go, cobrindo rotas, autenticação, mutações de negócio, exportação/importação, geração de PDFs, paridade de esquema entre SQLite e Postgres e o canal de WhatsApp.

Executar os testes:
```bash
go test ./...
```

Verificação estática:
```bash
go vet ./...
```

Os testes de banco rodam contra os dois dialetos (SQLite em memória/arquivo temporário e Postgres quando disponível), sem tocar no banco de desenvolvimento/produção.

---

## 📁 Estrutura do Projeto

```
aluxen/
├── cmd/                        # Pontos de entrada (binários)
│   ├── app/                    # Composition root do servidor (binário de produção)
│   ├── provision/              # CLI de provisionamento de instância no Easypanel
│   └── migrate/                # CLI de migração SQLite → Postgres
├── internal/
│   ├── domain/                 # Entidades, regras e catálogo de auditoria (sem I/O)
│   │   └── format/             # Formatação de moeda, datas BR, CPF, CEP e telefone
│   ├── application/            # Casos de uso (chat, WhatsApp, recibos, contratos, IA, jobs)
│   └── infrastructure/
│       ├── web/                # Rotas, handlers, middlewares, templates e estáticos
│       │   ├── templates/      # Telas do painel, portal e validação (html/template)
│       │   └── static/         # CSS e assets embutidos via embed
│       ├── db/                 # Adaptadores SQLite e Postgres + sqlc + paridade
│       │   ├── sqlite/         # Driver e queries SQLite
│       │   ├── postgres/       # Driver e queries Postgres
│       │   └── parity/         # Teste de paridade de esquema entre os dialetos
│       ├── pdf/                # Montagem e desenho dos documentos (fpdf)
│       ├── whatsapp/           # Cliente WAHA, pareamento, webhook e fila
│       ├── ai/                 # Cliente do provedor de IA (leitura de documento)
│       ├── authz/              # Tokens HMAC de autenticidade dos documentos
│       ├── config/             # Configuração por ambiente (12-factor)
│       ├── logging/            # Logger estruturado
│       ├── storage/            # Armazenamento de anexos
│       └── easypanel/          # Cliente da API de provisionamento
├── migrations/
│   ├── sqlite/                 # Migrações do dialeto SQLite
│   └── postgres/               # Migrações do dialeto Postgres (paridade de esquema)
├── queries/                    # Queries SQL usadas pelo sqlc
├── schema/                     # Esquema de referência (SQLite/Postgres)
├── docs/                       # Documentação técnica (ver seção abaixo)
├── Dockerfile                  # Imagem multi-stage → distroless
├── docker-compose.yml          # Orquestração com volume persistente em /data
└── .env.example                # Contrato das variáveis de ambiente
```

> O app **Bun legado** permanece no repositório apenas como referência histórica:
> `src/`, `public/`, `drizzle/`, `test/`, `package.json` e `tsconfig.json`. Não é
> o app vigente neste branch.

---

## 📚 Documentação Técnica Aprofundada

Para detalhes de engenharia de cada subsistema, consulte os manuais da pasta `docs/`:

- **[Arquitetura do port Go (`docs/ARQUITETURA-GO.md`)](docs/ARQUITETURA-GO.md)**: contrato normativo do port — camadas, dependências, mapa de rotas e funcionalidades novas.
- **[Deploy por instância (`ARQUITETURA-DEPLOY.md`)](ARQUITETURA-DEPLOY.md)**: uma instância por cliente no Easypanel, SQLite/Postgres e provisionamento.
- **[WhatsApp (`docs/WHATSAPP-GO.md`)](docs/WHATSAPP-GO.md)**: modos demo/cliente, configuração do canal (WAHA / Cloud API), webhook e endpoints.
- **[Validação pública (`docs/VALIDACAO-GO.md`)](docs/VALIDACAO-GO.md)**: protocolo HMAC, URL pública e a **dependência de hospedagem contínua** do QR Code.
- **[Privacidade / LGPD (`docs/PRIVACIDADE.md`)](docs/PRIVACIDADE.md)**: dados tratados, finalidades, controlador/operador, retenção e direitos do titular.
- **[Catálogo de APIs e Rotas HTTP (`docs/API.md`)](docs/API.md)**: rotas, query parameters, payloads e códigos de status HTTP.

**Documentos legados (app Bun)** — mantidos como referência histórica: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), [`docs/WHATSAPP.md`](docs/WHATSAPP.md) e [`docs/VALIDATION.md`](docs/VALIDATION.md).

---

## 📄 Licença

Este projeto é de código fechado e de propriedade de **Aluxen**. Todos os direitos reservados.
