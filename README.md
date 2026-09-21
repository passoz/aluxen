# Aluxen

Sistema completo e moderno de **gestão de locações imobiliárias**, executando com altíssima performance no runtime **Bun**, com o driver nativo **`bun:sqlite`**, **Hono**, **Drizzle ORM**, renderização híbrida **React SSR com hidratação**, geração dinâmica de **PDFs com autenticidade digital (QR Code)** e **Robô Headless de WhatsApp** integrado.

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
  - [8. Integração e Robô do WhatsApp Web](#8-integração-e-robô-do-whatsapp-web)
  - [9. Configurações do Locador & URL Pública](#9-configurações-do-locador--url-pública)
  - [10. Backup & Restauração JSON](#10-backup--restauração-json)
  - [11. Histórico & Auditoria](#11-histórico--auditoria)
  - [12. PWA, Responsividade & Sincronização Offline](#12-pwa-responsividade--sincronização-offline)
  - [13. Observabilidade & Logs Estruturados](#13-observabilidade--logs-estruturados)
  - [14. Área do Cliente (Portal do Locatário)](#14-área-do-cliente-portal-do-locatário)
  - [15. Informações do Sistema & Build em Execução](#15-informações-do-sistema--build-em-execução)
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

O **Aluxen** foi desenvolvido para locadores e administradores de imóveis que buscam autonomia, agilidade e segurança jurídica. O sistema elimina dependências pesadas de servidores tradicionais ou bancos de dados externos: com apenas um binário do Bun e um arquivo SQLite, você tem em mãos uma aplicação completa para cadastrar inquilinos, controlar contratos, emitir cobranças, gerar recibos em PDF com validação criptográfica e notificar locatários automaticamente via WhatsApp.

---

## 🛠 Stack Tecnológica

| Componente | Tecnologia | Finalidade |
|---|---|---|
| **Runtime** | [Bun](https://bun.sh/) (v1.4+) | Execução nativa ultrarrápida de TypeScript e empacotamento (`Bun.build`). |
| **Framework Web** | [Hono](https://hono.dev/) (v4+) | Roteamento leve, middlewares de sessão, rate limit e serving estático. |
| **Banco de Dados** | `bun:sqlite` | Driver SQLite nativo integrado ao runtime, com suporte a modo WAL. |
| **ORM & Migrações** | [Drizzle ORM](https://orm.drizzle.team/) | Consultas tipadas ponta a ponta e histórico versionado de migrações. |
| **Frontend & SSR** | [React 19](https://react.dev/) + React DOM Server | SSR instantâneo no servidor e hidratação interativa no cliente. |
| **Estilização** | CSS puro (`globals.css`) | Design glassmorphism moderno, responsivo, com temas Claro e Escuro. |
| **Geração de PDF** | [`pdf-lib`](https://pdf-lib.js.org/) | Geração em memória de recibos de aluguel e contratos completos de 25 cláusulas. |
| **QR Code** | `qrcode` / `qrcode-terminal` | Geração de QR Codes dinâmicos para PDFs, portal web e terminal CLI. |
| **Robô WhatsApp** | [`puppeteer-core`](https://pptr.dev/) + Chromium | Disparo automatizado de mensagens e comprovantes em segundo plano (Headless). |
| **Criptografia** | `node:crypto` + `bcryptjs` | Hash de senhas (bcrypt), tokens de autenticidade HMAC-SHA256 e `timingSafeEqual`. |

---

## 🎯 Funcionalidades Principais

### 1. Autenticação & Segurança
- **Sessões HTTP-only Seguras**: Armazenadas em cookie criptografado (`__aluxen_session`), com flags `HttpOnly`, `SameSite=Lax`, e validade de 30 dias.
- **Duas autenticações independentes**: o painel do locador (`__aluxen_session`) e a **Área do Cliente** do locatário (`__aluxen_client_session`) usam cookies distintos, sem privilégios cruzados — a sessão do locatário não abre nenhuma rota administrativa.
- **Proteção contra Força Bruta**: Rate limiting em memória por IP (padrão de 5 tentativas por minuto, configurável via `RATE_LIMIT_MAX`).
- **Hashes Criptográficos**: Senhas protegidas com `bcryptjs` (salt cost 10).
- **Usuário Administrador Automático**: Semeado no boot caso nenhum usuário exista (`passoz` / `senha-de-teste`, customizável via variáveis de ambiente).

### 2. Gestão de Inquilinos
- Cadastro completo de locatários: Nome, CPF formatado, Email e Telefone/WhatsApp com DDD.
- Busca em tempo real e ordenação multi-coluna com paginação.
- Modais rápidos de cadastro e edição.
- **Exclusão Segura**: O sistema impede a exclusão acidental de locatários que possuam contratos ativos.

### 3. Gestão de Imóveis
- Controle de propriedades residenciais e comerciais (Nome, Endereço, Cidade, Estado/UF, CEP).
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
  - Assinatura digital do locador (configurável via `&assinatura=false` para recibo em branco).
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

### 8. Integração e Robô do WhatsApp Web
- **Envio Direto (`wa.me`)**: Gera link oficial para envio manual pelo aplicativo ou WhatsApp Web.
- **Robô Headless Automatizado**:
  - Controla o WhatsApp Web em segundo plano via **Puppeteer Core + Chromium**.
  - **Pareamento Simples via QR Code**: Pode ser realizado diretamente pelo modal no painel web (`/settings` ou tela de envio) ou pelo terminal com `bun run whatsapp:login`.
  - **Persistência de Sessão**: Armazena o perfil autenticado em `.whatsapp-profile/`, mantendo o robô conectado mesmo após reiniciar a máquina ou o container Docker.
  - Mensagens pré-formatadas com emojis, resumo financeiro, link para download do PDF e link do portal de validação.

### 9. Configurações do Locador & URL Pública
- Cadastro de Nome, CPF e Email do Locador para emissão automática nos cabeçalhos de contratos e recibos.
- Configuração do Domínio / URL Pública do Sistema (`app_url`), permitindo apontar para o domínio de produção oficial (`https://aluxen.evolucsia.com`) ou domínio próprio.
- Painel de status e reconexão do Robô do WhatsApp em tempo real.
- Alteração segura de senha do usuário autenticado.

### 10. Backup & Restauração JSON
- **Exportação Completa (`/export`)**: Gera download imediato de arquivo `aluxen-backup-YYYY-MM-DD.json` contendo todas as tabelas (inquilinos, imóveis, contratos, recibos e configurações).
- **Restauração / Importação (`/import`)**: Upload do arquivo JSON para restaurar ou migrar a base inteira em uma transação atômica.

### 11. Histórico & Auditoria
- **Trilha de auditoria completa (`audit_logs`)**: toda operação relevante é registrada com autor, data/hora, IP, protocolo da requisição e o **diff campo a campo** (valor antes → valor depois) já formatado em português.
- Cobre cadastros e alterações de inquilinos e imóveis, criação/alteração/encerramento de contratos, lançamentos, baixas (individuais e em lote), geração mensal, alteração de valores e de dados do locador, troca de senha, **emissões de PDF** (recibo e contrato), envios pelo WhatsApp, backups e acessos (login, tentativas negadas e saída).
- **Aba "Histórico" no painel**: filtros por área, ação, usuário e período, busca textual, ordenação cronológica, paginação, detalhe do evento com tabela *antes/depois* e **exportação CSV** (compatível com Excel em português).
- Senhas, tokens e cookies **nunca** são gravados na trilha.

### 12. PWA, Responsividade & Sincronização Offline
- Suporte a Progressive Web App com manifesto (`/manifest.webmanifest`) e ícones em alta resolução para instalação no celular (iOS e Android).
- **Fila de Sincronização Offline**: Ações realizadas sem conexão à internet são retidas em `localStorage` e reenviadas ao servidor automaticamente assim que a conectividade for restabelecida.
- Sidebar retrátil com persistência no navegador e Drawer mobile para operação confortável em telas pequenas.
- Seletor de tema **Claro / Escuro** integrado.

### 13. Observabilidade & Logs Estruturados
- **Logger estruturado** (`src/utils/logger.ts`): uma linha por evento com timestamp ISO, nível (`debug`, `info`, `warn`, `error`), escopo (`http`, `audit`, `db`, `whatsapp`, `app`) e campos tipados.
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
- **Identificação do build**: origem (`Imagem publicada (GHCR)` ou `Execução local`), versão do `package.json`, commit, branch/tag, **tag da imagem** (`ghcr.io/passoz/aluxen:sha-<commit>`), data de construção (UTC) e link direto para a execução do GitHub Actions que gerou a imagem.
- **Estado do sistema**: runtime do Bun, ambiente, fuso horário, hora de início e tempo no ar, caminho e tamanho do banco SQLite, URL pública configurada e contagem de inquilinos, imóveis, contratos e recibos.
- Os metadados são injetados na imagem pelo workflow de publicação (`build-args` → `ENV` no `Dockerfile`) e lidos em runtime por `src/utils/build-info.ts`; fora da imagem, o sistema cai para o repositório git local. Sem metadados, o modal avisa que a execução não é identificável.

---

## 🚀 Guia Rápido de Instalação e Execução

### Pré-requisitos
- [Bun](https://bun.sh/) (v1.1 ou superior).
- Google Chrome ou Chromium instalado (necessário apenas caso deseje utilizar o **Robô Headless** do WhatsApp).

### 1. Clonar o repositório
```bash
git clone https://github.com/passoz/aluxen.git
cd aluxen
```

### 2. Instalar dependências
```bash
bun install
```

### 3. Compilar o bundle de cliente
```bash
bun run build
```

### 4. Executar em modo desenvolvimento
```bash
bun run dev
```
O servidor estará acessível em:
- **Navegador local**: `http://localhost:3000`
- **Pelo smartphone no mesmo Wi-Fi**: `http://<SEU-IP-LOCAL>:3000` (exibido no terminal ao iniciar)

### 5. Executar em modo produção
```bash
bun run build
bun run start
```

### Credenciais Padrão no Primeiro Acesso
- **Painel administrativo**: usuário `passoz` / senha `senha-de-teste`.
- **Área do Cliente (locatário)** — `/portal/login`: usuário e senha são o **CPF do locatário sem pontuação** (ex.: CPF `529.982.247-25` → login `52998224725` e senha `52998224725`). A troca de senha é exigida no primeiro acesso. Locatários sem CPF cadastrado não conseguem entrar — complete o cadastro em *Inquilinos*.

---

## ⚙️ Variáveis de Ambiente

O Aluxen funciona perfeitamente com valores padrão pré-configurados, mas você pode customizá-lo através de um arquivo `.env` na raiz do projeto:

| Variável | Padrão | Descrição |
|---|---|---|
| `PORT` | `3000` | Porta TCP na qual o servidor HTTP do Hono irá escutar. |
| `HOST` | `0.0.0.0` | Endereço de bind de rede. |
| `NODE_ENV` | `development` | Ambiente de execução (`development` ou `production`). |
| `DATABASE_URL` | *(vazio)* | Caminho customizado para o banco (ex.: `file:/caminho/banco.db`). |
| `DB_DIR` | *(raiz)* | Diretório onde o arquivo `aluxen.db` e `.whatsapp-profile` serão gravados. |
| `SESSION_SECRET` | `aluxen-session-secret...` | Chave secreta usada para assinar cookies e gerar tokens HMAC de validação de documentos. |
| `APP_URL` | *(auto-detectado)* | URL pública base do sistema para links e QR Codes (ex.: `https://aluxen.evolucsia.com`). |
| `WHATSAPP_PROFILE_DIR` | `.whatsapp-profile` | Diretório onde os dados da sessão do WhatsApp Web são gravados. |
| `CHROMIUM_PATH` | *(auto-detectado)* | Caminho explícito para o executável do Chromium ou Google Chrome. |
| `RATE_LIMIT_MAX` | `5` | Número máximo de tentativas de login permitidas por IP por minuto. |
| `SEED_USERNAME` | `passoz` | Nome de usuário criado automaticamente no primeiro boot do banco. |
| `SEED_PASSWORD` | `senha-de-teste` | Senha inicial do usuário administrador. |
| `DEFAULT_LANDLORD_NAME`| `Locador` | Nome padrão do locador inserido nas configurações iniciais. |
| `DEFAULT_LANDLORD_CPF` | `""` | CPF inicial do locador. |
| `LOG_LEVEL` | `info` | Nível mínimo dos logs: `debug`, `info`, `warn`, `error` ou `silent`. |
| `LOG_FORMAT` | `json` fora de TTY, `pretty` em terminal | Formato de saída do logger (`json` para JSON Lines ou `pretty` para leitura humana). |
| `ALUXEN_BUILD_COMMIT`, `ALUXEN_BUILD_COMMIT_SHORT`, `ALUXEN_BUILD_REF`, `ALUXEN_BUILD_TIME`, `ALUXEN_BUILD_IMAGE`, `ALUXEN_BUILD_RUN_URL` | *(injetados na imagem)* | Metadados do build exibidos no modal **Informações**. Preenchidos pelo workflow de publicação via `build-args`; podem ser definidos manualmente para identificar uma execução local. |

---

## 📜 Scripts Disponíveis

Todos os comandos podem ser executados através do `bun run <script>`:

```bash
# Desenvolvimento com recarga automática ao alterar arquivos
bun run dev

# Compilar o bundle JavaScript do cliente React para public/dist/client.js
bun run build

# Iniciar o servidor em produção
bun run start

# Executar a suíte completa de testes automatizados
bun test

# Validação estática de tipos TypeScript (sem emitir arquivos)
bun run typecheck

# Iniciar login no WhatsApp Web pelo terminal via QR Code ASCII
bun run whatsapp:login

# Aplicar migrações pendentes no banco SQLite
bun run db:migrate

# Gerar novas migrações a partir de alterações em src/db/schema.ts
bun run db:generate

# Sincronizar o schema diretamente com o banco de dados
bun run db:push

# Abrir o Drizzle Studio (painel web interativo para inspecionar o banco)
bun run db:studio

# Popular o banco com dados demonstrativos para testes
bun run db:seed
```

---

## 🗄 Banco de Dados & Migrações

O Aluxen utiliza o **Drizzle ORM** configurado para o driver nativo **`bun:sqlite`**. O banco opera em modo **WAL (Write-Ahead Logging)** com chaves estrangeiras ativadas.

- As definições de tabelas e tipos residem em `src/db/schema.ts`.
- Os arquivos SQL de migração residem em `drizzle/`.
- No boot da aplicação, a função `initializeDatabase()` em `src/db/index.ts` executa automaticamente todas as migrações necessárias e realiza checagens idempotentes, garantindo compatibilidade retroativa automática caso venha de versões anteriores do banco.

---

## 🐳 Execução com Docker e Docker Compose

O Aluxen possui suporte nativo a Docker com **Chromium e fontes brasileiras pré-instalados**, permitindo que o Robô do WhatsApp funcione perfeitamente dentro do container sem nenhuma configuração extra.

### Subir com Docker Compose:
```bash
docker compose up -d --build
```
O serviço estará acessível em `http://localhost:3000`, e os dados do SQLite e a sessão do WhatsApp serão persistidos no volume nomeado `aluxen-data`.

### Imagem Docker Oficial
A imagem de produção oficial é publicada automaticamente via GitHub Actions no Docker Hub:
```bash
docker pull passoz/aluxen:latest
docker run -d -p 3000:3000 -v /minha-pasta/data:/data passoz/aluxen:latest
```

Cada build é publicado com duas tags: `latest` e `sha-<commit>`. O commit, a branch, a data de construção e a tag exata da imagem ficam gravados dentro da própria imagem (`build-args` → `ENV`) e aparecem no painel em **Informações** — dá para confirmar qual build está rodando sem consultar o GitHub.

---

## 🧪 Testes e Qualidade de Código

A aplicação possui testes unitários e de integração de ponta a ponta construídos sobre o test runner nativo do Bun (`bun:test`), cobrindo rotas, autenticação, mutações de negócio, exportação/importação e geração de PDFs com validação.

Executar os testes:
```bash
bun run test
```

O script recria o banco de testes em `data-test/` (`DB_DIR=data-test`) a cada execução, de modo que a suíte nunca toca no banco de desenvolvimento/produção (`aluxen.db`).

Verificar a integridade dos tipos TypeScript:
```bash
bun run typecheck
```

---

## 📁 Estrutura do Projeto

```
aluxen/
├── Dockerfile                  # Imagem multi-stage de produção com Chromium
├── Dockerfile.dev              # Container para desenvolvimento com hot-reload
├── Dockerfile.test             # Container para execução de testes em CI
├── docker-compose.yml          # Orquestração do serviço com volume persistente
├── package.json                # Dependências, metadados e scripts de execução
├── tsconfig.json               # Configurações do compilador TypeScript
├── drizzle.config.ts           # Configuração do Drizzle Kit
├── drizzle/                    # Arquivos SQL de migração de banco versionados
├── docs/                       # Documentações técnicas detalhadas
│   ├── ARCHITECTURE.md         # Arquitetura interna, fluxo de dados e SSR
│   ├── WHATSAPP.md             # Manual completo do Robô WhatsApp Web e Headless
│   ├── VALIDATION.md           # Especificação do protocolo de autenticidade e QR Code
│   └── API.md                  # Catálogo completo de endpoints e mutations HTTP
├── public/                     # Arquivos estáticos servidos pelo Hono
│   ├── dist/client.js          # Bundle hidratado do React compilado via Bun.build
│   ├── styles/globals.css      # Folha de estilo global com glassmorphism e temas
│   ├── manifest.webmanifest    # Manifesto para instalação como PWA
│   ├── favicon.svg             # Ícone vetorial da marca
│   └── assets/assinatura01.png # Imagem modelo para carimbo de assinatura digital
├── src/                        # Código-fonte da aplicação
│   ├── index.tsx               # Ponto de entrada, rotas do Hono e handlers de SSR
│   ├── build-client.ts         # Script de bundling do cliente React via Bun.build
│   ├── client/                 # Componentes React de página hidratados no navegador
│   │   ├── index.tsx           # Entry point da hidratação com hydrateRoot
│   │   ├── dashboard.tsx       # Painel administrativo com abas e modais
│   │   ├── history.tsx         # Aba de Histórico e Auditoria (filtros e diff)
│   │   ├── login.tsx           # Tela de autenticação com tratamento de erros
│   │   ├── portal.tsx          # Área do Cliente: login, painel e troca de senha
│   │   ├── settings.tsx        # Configurações do locador, WhatsApp e senhas
│   │   └── verify.tsx          # Portal público de validação de documentos
│   ├── components/             # Componentes de interface compartilhados
│   │   ├── ui.tsx              # Modais, DatePicker, Select com busca, MoneyInput
│   │   └── icons.tsx           # Ícones SVG reutilizáveis
│   ├── db/                     # Camada de banco de dados
│   │   ├── index.ts            # Conexão SQLite, inicialização e seed
│   │   ├── schema.ts           # Definição das tabelas Drizzle ORM
│   │   ├── migrate.ts          # Executor de migrações automáticas
│   │   └── seed-demo.ts        # Dados demonstrativos para testes
│   ├── types/                  # Tipagens TypeScript compartilhadas
│   │   ├── dashboard.ts        # Tipos de entidades (Tenant, Property, Lease, Receipt)
│   │   ├── system.ts           # Payload do modal de Informações (build + estado)
│   │   └── http.ts             # Variáveis tipadas de request do Hono (requestId)
│   ├── utils/                  # Utilitários de sistema
│   │   ├── auth.ts             # Lógica de login e alteração de senha (bcryptjs)
│   │   ├── auth-token.ts       # Geração e checagem de tokens HMAC e QR Codes
│   │   ├── session.ts          # Gerenciamento do cookie de sessão HTTP-only
│   │   ├── build-info.ts       # Identificação do build em execução (imagem/git)
│   │   ├── tenant-auth.ts      # Credencial do locatário (login por CPF e troca de senha)
│   │   ├── logger.ts           # Logger estruturado (níveis, JSON/pretty, redação)
│   │   ├── audit.ts            # Trilha de auditoria: gravação, diffs e consultas
│   │   ├── audit-meta.ts       # Rótulos e formatação de eventos (cliente/servidor)
│   │   ├── format.ts           # Formatadores de moeda, datas BR, CPF e telefone
│   │   ├── id.ts               # Gerador de identificadores únicos UUIDv7
│   │   ├── pdf.ts              # Gerador do Recibo de Pagamento em PDF
│   │   ├── contract-pdf.ts     # Gerador do Contrato de Locação em PDF
│   │   └── offline.ts          # Utilitários para sincronização e fila offline
│   └── whatsapp/               # Módulo do Robô WhatsApp Web
│       ├── browser.ts          # Lançador e gerenciador do Puppeteer/Chromium
│       ├── login-manager.ts    # Máquina de estados do login com QR Code
│       ├── service.ts          # Templates de mensagens e disparos headless
│       ├── login.ts            # Script CLI para pareamento via terminal
│       └── types.ts            # Interfaces e estados de conexão
└── test/                       # Suíte de testes automatizados
    ├── app.test.ts             # Testes de integração de rotas, PDFs, APIs e auditoria
    ├── build-info.test.ts      # Identificação do build (Dockerfile, workflow e git)
    ├── logger.test.ts          # Testes do logger (níveis, formatos e redação)
    ├── datepicker.test.tsx     # Testes unitários do seletor de datas
    └── migration.test.ts       # Testes de integridade do esquema e migrações
```

---

## 📚 Documentação Técnica Aprofundada

Para detalhes de engenharia de cada subsistema específico, consulte os manuais disponíveis na pasta `docs/`:

- **[Arquitetura do Sistema (`docs/ARCHITECTURE.md`)](docs/ARCHITECTURE.md)**: Fluxo detalhado de requisições, SSR e hidratação React 19, SQLite WAL e modelo de entidades.
- **[Manual do Robô do WhatsApp (`docs/WHATSAPP.md`)](docs/WHATSAPP.md)**: Configuração do Chromium, máquina de estados do login, templates de mensagens e troubleshooting no Docker.
- **[Protocolo de Autenticidade & Validação (`docs/VALIDATION.md`)](docs/VALIDATION.md)**: Algoritmo de geração HMAC-SHA256, formatação de códigos de autenticidade e resolução de IP na rede local.
- **[Catálogo de APIs e Rotas HTTP (`docs/API.md`)](docs/API.md)**: Especificação completa de todas as rotas, query parameters, payloads de mutations e códigos de status HTTP.

---

## 📄 Licença

Este projeto é de código fechado e de propriedade de **Aluxen**. Todos os direitos reservados.
