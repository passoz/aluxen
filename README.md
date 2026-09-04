# Aluxen (Bun + SQLite + Hono + Drizzle ORM)

Port 1:1 em funcionalidade, comportamento e visual do sistema de gestão de locações imobiliárias Aluxen, executando nativamente em **Bun** com o driver nativo **`bun:sqlite`**, **Hono**, **Drizzle ORM**, e componentes de interface React/SSR com hidratação de alta performance.

## Stack

- **Runtime:** [Bun](https://bun.sh/)
- **Framework Web:** [Hono](https://hono.dev/)
- **Driver de Banco:** `bun:sqlite` (nativo do Bun)
- **ORM & Migrações:** [Drizzle ORM](https://orm.drizzle.team/)
- **Geração de PDF:** `pdf-lib` (Recibos com/sem assinatura e Contratos de Locação)
- **Frontend & UI:** React SSR + hidratação via `Bun.build`, estilização glassmorphism idêntica via `globals.css`
- **Autenticação:** Sessão em cookie HTTP-only (`__aluxen_session`) com senhas em `bcryptjs`

## Telas e Funcionalidades (1:1 com `main`)

1. **Autenticação (`/login`, `/logout`)**:
   - Rate limit configurável (máximo de 5 tentativas por minuto por IP).
   - Usuário admin padrão semeado automaticamente: `passoz` / `Dt32btop@` (configurável via `SEED_USERNAME` e `SEED_PASSWORD`).
   - Bloqueio de rotas protegidas e redirecionamento de usuários autenticados.

2. **Painel Principal (`/dashboard`)**:
   - **Inquilinos (`?tab=tenants`)**: listagem com paginação e ordenação, busca em tempo real, modal de cadastro, modal de edição, exclusão protegida (impede exclusão se houver contrato ativo).
   - **Imóveis (`?tab=properties`)**: listagem, busca, ordenação, paginação, cadastro e edição em modais, exclusão.
   - **Contratos (`?tab=leases`)**: listagem, filtro por status (Todos, Ativos, Encerrados), busca, ordenação, modais de cadastro e edição com date picker e select com busca, botões de Encerrar/Reativar contrato, e botão para emitir o Contrato de Locação formal em PDF (`/contract-pdf?id=...`).
   - **Pagamentos e Recibos (`?tab=receipts`)**: listagem, filtro por Mês, Ano e Status (Todos, Pagos, Vencidos, A vencer), seleção múltipla para pagamento em lote, botão "Gerar pagamentos" do mês atual, cadastro e edição de recibos, botão de baixa individual, e emissão de recibos em PDF (com assinatura digital e sem assinatura).
   - **Sidebar Retrátil**: menu colapsável com persistência em `localStorage`, drawer mobile responsivo, alternância de tema Claro/Escuro com persistência, e botões de exportação e importação.

3. **Configurações (`/settings`)**:
   - Edição dos dados cadastrais do Locador (Nome, CPF, Email) para preenchimento automático em contratos e recibos.
   - Alteração de senha do usuário autenticado.

4. **Emissão de Documentos em PDF**:
   - `/pdf?id=...`: Recibo de aluguel formatado, com valores monetários por extenso em português, data por extenso e assinatura digital configurável (`&assinatura=false` para recibo sem assinatura).
   - `/contract-pdf?id=...`: Contrato de Locação Residencial completo com 25 cláusulas legais, dados das partes e campos para assinaturas de locador, locatário, cônjuges e testemunhas.

5. **Backup & Restauração**:
   - `/export`: Download do backup completo em JSON (`aluxen-backup-YYYY-MM-DD.json`).
   - `/import`: Restauração e sincronização dos registros via upload do arquivo JSON de backup.

6. **Suporte Offline / PWA**:
   - Fila de sincronização offline (`offline-queue`) para enfileirar alterações feitas offline e sincronizá-las automaticamente com o servidor quando a conexão for restabelecida.

## Como Rodar

### Instalação
```bash
bun install
```

### Desenvolvimento
```bash
bun run dev
```
O servidor estará acessível em `http://localhost:3000`.

### Build do Cliente
```bash
bun run build
```

### Produção
```bash
bun run start
```

### Testes & Verificação de Tipos
```bash
bun test
bun run typecheck
```
