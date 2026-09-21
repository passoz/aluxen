# Operação do Aluxen (Go) — uma instância por cliente

Roteiro de operação das instâncias: provisionar, expirar demo, migrar de dialeto,
atualizar, fazer backup e diagnosticar. Escrito para ser repetido sem depender de
memória — todo passo aqui é um comando versionado.

O desenho está em [`ARQUITETURA-DEPLOY.md`](../ARQUITETURA-DEPLOY.md); o contrato do
port está em [`ARQUITETURA-GO.md`](ARQUITETURA-GO.md).

---

## 0. O modelo em uma tela

```
Easypanel (VPS)
└── projeto aluxen
    ├── app: aluxen-passoz       → aluxen.evolucsia.com         volume: /data
    ├── app: aluxen-demo-buzios  → buzios.aluxen.evolucsia.com  volume: /data
    └── app: aluxen-cliente-x    → x.aluxen.evolucsia.com       volume: /data
```

- **Uma instância = um cliente ou uma demo.** Container, volume e subdomínio próprios.
  Não existe banco compartilhado, então não existe resíduo em outro lugar.
- **Imagem:** `ghcr.io/passoz/aluxen:sha-<commit>`, publicada pelo workflow. A VPS só
  faz *pull* — nada é construído nela.
- **Persistência:** volume montado em `/data`, com `DB_DIR=/data`. SQLite por padrão
  (`/data/aluxen.db`); Postgres só quando `DATABASE_URL` aponta para lá.
- **Domínio e TLS:** o Easypanel emite o certificado (Let's Encrypt/Traefik). O
  **registro DNS é manual**.
- **Limite do plano:** 3 *projetos*, não apps. Todas as instâncias convivem no mesmo
  projeto.

---

## 1. Provisionar uma instância

### Pré-requisitos

1. **Imagem publicada.** O workflow de publicação gera a tag `sha-<commit-curto>`.
   Sem a tag publicada, o deploy sobe e falha no *pull*.
2. **Credenciais da API do painel no ambiente do operador** (nunca no código, nunca
   versionadas):

   ```sh
   export EASYPANEL_URL="https://painel.exemplo.com"
   export EASYPANEL_API_KEY="cole-aqui-o-token-do-painel"
   export EASYPANEL_PROJECT="aluxen"   # opcional: este é o padrão
   ```

3. **DNS.** Saiba qual subdomínio será usado: o comando publica o domínio, mas o
   registro DNS é feito por você depois.

### Comando

```sh
go run ./cmd/provision create \
  --name aluxen-demo-buzios \
  --domain buzios.aluxen.evolucsia.com \
  --image ghcr.io/passoz/aluxen:sha-abc1234 \
  --db-dir /data \
  --seed-demo \
  --expires-at +30d
```

### Flags

| Flag | Padrão | Para que serve |
|---|---|---|
| `--name` | — | Nome do serviço no painel. **Obrigatória.** Use `aluxen-<cliente>` ou `aluxen-demo-<praia>`. |
| `--domain` | — | Subdomínio da instância. **Obrigatória.** |
| `--image` | — | Imagem publicada no GHCR. **Obrigatória.** |
| `--project` | `$EASYPANEL_PROJECT` ou `aluxen` | Projeto do painel. |
| `--db-dir` | `/data` | Diretório do banco dentro do volume. |
| `--port` | `3000` | Porta do app no container (a mesma do `Dockerfile`). |
| `--seed-demo` | `false` | Sobe a instância já com os dados de demonstração (`SEED_DEMO=true`). |
| `--seed-username` | `$SEED_USERNAME` ou `passoz` | Administrador semeado. |
| `--seed-password` | `$SEED_PASSWORD` ou `senha-de-teste` | Senha semeada (troca obrigatória no primeiro acesso). |
| `--session-secret` | `$SESSION_SECRET` ou **gerado** | Assina sessão, CSRF e os tokens dos documentos. |
| `--expires-at` | — | Etiqueta de expiração: `AAAA-MM-DD` ou `+Nd`. |

### O que o comando faz

1. **App** — `createAppService` com a fonte `image` (nenhum build na VPS).
2. **Volume** — `createMount` com volume nomeado `<name>-data` em `--db-dir`.
3. **Variáveis** — `updateAppEnv` com `APP_ENV=production`, `HOST`, `PORT`, `TZ`,
   `LOG_FORMAT=json`, `DB_DIR`, `SESSION_SECRET`, `SEED_USERNAME`, `SEED_PASSWORD`,
   `SEED_DEMO` e `APP_URL=https://<domain>`. As variáveis de IA e de WhatsApp são
   repassadas do seu ambiente quando existirem (ver §6).
4. **Domínio** — `createDomain` com `https: true` e `certificateResolver: letsencrypt`.
5. **Deploy** — `deployAppService`.
6. **Etiqueta** — anotação `expires_at=AAAA-MM-DD` no serviço, quando `--expires-at`
   for informado (ver §2).

### Saída esperada

```
projeto:   aluxen
instância: aluxen-demo-buzios
imagem:    ghcr.io/passoz/aluxen:sha-abc1234
volume:    aluxen-demo-buzios-data em /data
domínio:   https://buzios.aluxen.evolucsia.com
demo:      sim
expira em: 2026-10-21

  [criado    ] serviço de aplicação aluxen-demo-buzios
  [criado    ] volume aluxen-demo-buzios-data em /data
  [atualizadas] variáveis da instância
  [criado    ] domínio buzios.aluxen.evolucsia.com
  [disparado ] deploy
  [gravada   ] etiqueta de expiração 2026-10-21

próximo passo: aponte o DNS de buzios.aluxen.evolucsia.com para a VPS (registro manual).
```

Se uma etapa falhar, o comando para ali e mostra o motivo com o status HTTP da API
(`[falhou] ...: easypanel: createDomain: HTTP 400: ...`).

### Reexecutar com segurança (idempotência)

O comando é **convergente**: reexecutar o mesmo `create` não duplica nada.

- O serviço já existe → **não recria**; a **imagem é ajustada** para a informada.
- O volume já existe em `--db-dir` → não monta de novo.
- O domínio já está publicado → não cria de novo.
- As **variáveis são reescritas** com o estado declarado (a instância é do
  provisionamento). Se você adicionou uma variável à mão no painel, ela **não**
  sobrevive a uma reexecução — coloque-a no ambiente do operador (§6) para ela ser
  repassada.
- O deploy é disparado de novo (é o que aplica as mudanças).

Depois do DNS propagar:

```sh
curl -fsS https://buzios.aluxen.evolucsia.com/healthz   # → ok
curl -fsS https://buzios.aluxen.evolucsia.com/readyz    # → ready
```

---

## 2. Ciclo de vida da demo

### A etiqueta de expiração

A data vive na **anotação do serviço** no painel (`setServiceNotes`/`getServiceNotes`),
no formato `expires_at=AAAA-MM-DD`. É o inventário da demo: não há planilha paralela.

- Gravada pelo `create --expires-at <data>`.
- Lida pelo `list` e usada pelo `expire`.
- **Serviço sem etiqueta nunca expira sozinho** — um cliente jamais é removido por
  engano.

### Listar

```sh
go run ./cmd/provision list
```

```
SERVIÇO               EXPIRA      SITUAÇÃO
aluxen-demo-buzios    2026-10-21  vigente
aluxen-demo-arraial   2026-09-01  vencida
aluxen-passoz         -           sem prazo
```

### Expirar

```sh
# ensaio: mostra o que sairia, sem remover nada
go run ./cmd/provision expire --days 0 --dry-run

# remove o que já venceu
go run ./cmd/provision expire --days 0

# antecipa a limpeza para quem vence dentro de 7 dias
go run ./cmd/provision expire --days 7

# remove o que vence até uma data
go run ./cmd/provision expire --expires-at 2026-10-05
```

- `--days N` remove o que vence em até N dias **e** o que já venceu (`0` = só o
  vencido).
- `--expires-at DATA` vale até o fim do dia informado.
- `--days` e `--expires-at` são exclusivos entre si.

### Ordem da remoção (sem resíduo)

Para cada demo vencida o comando **remove os volumes primeiro e o serviço depois**
(`deleteMount` → `destroyAppService`). Assim o volume de dados não fica órfão no
painel. Nada fica em nenhum outro lugar — não existe banco compartilhado.

### Cron sugerido

```cron
# todo dia às 6h: remove as demos vencidas
0 6 * * * cd /opt/aluxen && EASYPANEL_URL=... EASYPANEL_API_KEY=... \
  go run ./cmd/provision expire --days 0 >> /var/log/aluxen-expire.log 2>&1
```

---

## 3. Migrar de dialeto (SQLite → Postgres)

Troca uma instância de SQLite para Postgres com pré-checagem, dry-run, cópia em
transação e verificação. **A origem nunca é apagada** — o volume fica como backup.

### Pré-requisitos

- O arquivo SQLite da instância (dentro do volume: `/data/aluxen.db`).
- Um Postgres **novo, vazio**, com o DSN à mão. O comando aplica as migrações
  pendentes no destino antes de copiar, então não é preciso subir o app apontando
  para ele.

### Comando

```sh
# 1. Ensaio: conta as linhas por tabela e não escreve nada no destino.
go run ./cmd/migrate \
  --from file:/data/aluxen.db \
  --to 'postgres://usuario:senha@host:5432/aluxen' \
  --dry-run

# 2. Cópia de verdade.
go run ./cmd/migrate \
  --from file:/data/aluxen.db \
  --to 'postgres://usuario:senha@host:5432/aluxen'
```

O relatório traz uma linha por tabela com `ORIGEM`, `DESTINO` e `CHECKSUNK`
(`confere` ou `DIVERGE`), e o total. Em falha, o relatório aponta **onde parou** e
que o destino foi revertido.

### O que acontece por dentro

1. **Pré-checagem** — origem legível e com o schema final; destino acessível,
   migrado e **vazio**. Destino com qualquer linha é recusado: a cópia não tem
   upsert e duplicar seria silencioso.
2. **Dry-run** (`--dry-run`) — contagem e checksum por tabela, sem escrever.
3. **Cópia em uma única transação**, na ordem de dependência das chaves
   estrangeiras:

   ```
   users → properties → tenants → leases → receipts → settings → audit_logs
         → inspections → whatsapp_messages → jobs → payment_proofs → document_intakes
   ```

4. **Verificação** — depois de cada tabela, contagem e SHA-256 do conteúdo
   (linhas ordenadas pela chave primária, hash de todas as colunas) são comparados
   com a origem, dentro da transação.
5. **Rollback** — qualquer erro de cópia ou divergência reverte a transação inteira:
   o destino não fica parcialmente consistente. O commit só acontece com as 12
   tabelas conferindo.

A origem é aberta em modo somente consulta (`query_only`), então a migração não
escreve nem apaga nada nela.

### Depois da cópia (passos do operador)

1. Na instância, troque a variável de ambiente:

   ```
   DATABASE_URL=postgres://usuario:senha@host:5432/aluxen
   ```

   (no lugar de `DB_DIR=/data` — deixe `DB_DIR` como está, ele só é usado quando
   `DATABASE_URL` está vazia.)
2. **Redeploy** da instância.
3. Confirme: `curl -fsS https://<instância>/readyz` → `ready`, login e uma aba do
   painel.
4. **Não apague o SQLite.** O arquivo no volume é o backup e a volta atrás.

### Como voltar atrás

Volte `DATABASE_URL` para o valor anterior (vazio) e faça o redeploy. A instância
volta a ler o SQLite do volume, que nunca foi tocado. As escritas feitas no
Postgres nesse intervalo não voltam — por isso a janela de teste é curta.

### Observações

- A cópia é uma transação única: em base muito grande, é uma transação longa.
  Para o porte atual (uma instalação, poucos milhares de linhas) é irrelevante.
- Para ensaiar contra um Postgres de teste, o mesmo comando serve; a suíte de
  testes da migração usa `TEST_DATABASE_URL` quando quiser exercitar o dialeto real.

---

## 4. Atualização canário

Uma instância primeiro, as outras depois. O mesmo `create` é o caminho da
atualização: a imagem é convergida e o deploy disparado.

```sh
# 1. Um cliente só.
go run ./cmd/provision create \
  --name aluxen-passoz \
  --domain aluxen.evolucsia.com \
  --image ghcr.io/passoz/aluxen:sha-novo123
```

Valide, nesta ordem:

1. `curl -fsS https://aluxen.evolucsia.com/healthz` → `ok` (liveness, não toca o banco).
2. `curl -fsS https://aluxen.evolucsia.com/readyz` → `ready` (readiness, faz ping no banco).
3. Login do administrador.
4. Uma aba que use o que mudou (painel, contratos, pagamentos, temporada, revisão).

Passando, repita o mesmo comando para as outras instâncias, uma por vez.

### Se a atualização quebrar no boot

O log JSON da instância diz onde parou (ver §7). Dois casos comuns:

- **Erro de migração** (`aplicar migrações de ...`) — o processo sai com
  `falha ao iniciar: ...`. Volte a imagem anterior (reexecute `create` com a tag
  antiga) e investigue fora da produção.
- **Processo sobe mas `/readyz` responde 503** — o banco não respondeu ao ping
  (`banco indisponível` no log). Confira `DATABASE_URL`/`DB_DIR` e o volume montado.

Rollback de imagem é o mesmo comando com a tag anterior:

```sh
go run ./cmd/provision create --name aluxen-passoz --domain aluxen.evolucsia.com \
  --image ghcr.io/passoz/aluxen:sha-anterior
```

---

## 5. Backup e restauração por volume

### O que é o volume

Cada instância tem um volume montado em `/data` (o `--db-dir` do provisionamento):

```
/data/aluxen.db        ← banco SQLite
/data/aluxen.db-wal    ← write-ahead log (existe enquanto o app roda)
/data/aluxen.db-shm    ← índice do WAL
/data/storage/         ← fotos de vistoria e documentos enviados
```

O volume é a instância: copiá-lo é copiar o cliente; apagá-lo é apagar o cliente.

### Backup (SQLite)

O SQLite roda em modo WAL, então **não copie só o `.db` com o app rodando**: o que
ainda está no WAL se perde. Duas formas seguras:

```sh
# A) com o app parado (mais simples e sempre correto)
#    pare o serviço no painel e copie o arquivo
cp "$VOLUME/aluxen.db" "backup-aluxen-$(date +%F).db"

# B) com o app rodando, se houver o cliente sqlite3 na VPS
sqlite3 "$VOLUME/aluxen.db" ".backup '/tmp/aluxen-$(date +%F).db'"
```

Em ambos os casos inclua `storage/` no backup (tar é o mais prático):

```sh
tar czf "aluxen-$(date +%F).tar.gz" -C "$VOLUME" aluxen.db storage
```

`$VOLUME` é a pasta do volume do serviço na VPS — o painel mostra o caminho no
serviço, na aba de montagens.

### Restauração (SQLite)

1. Pare a instância no painel.
2. Troque o arquivo (e o `storage/`):

   ```sh
   rm -f "$VOLUME/aluxen.db-wal" "$VOLUME/aluxen.db-shm"
   cp backup-aluxen-2026-09-21.db "$VOLUME/aluxen.db"
   ```

3. Suba a instância e confira `/readyz` e o login.

**Por que isso funciona sem rodar migração:** no boot o app compara o schema que
encontrou com o que cada migração embutida produz. Uma base que **já está no schema
final** tem as migrações apenas **registradas como aplicadas** — nada é recriado e
nenhuma linha se perde. É o mesmo caminho de quem copia o volume de uma instância
para outra. Restaurar um dump num banco vazio, portanto, é suportado.

Se o backup vier de uma versão **anterior** do schema, o boot aplica o que falta
(o goose roda normalmente depois da adoção).

### Backup e restauração (Postgres)

Quando a instância usa Postgres, o volume não tem o banco — o backup é do servidor:

```sh
pg_dump "$DATABASE_URL" -Fc -f "aluxen-$(date +%F).dump"          # backup
pg_restore -d "$DATABASE_URL" --clean --if-exists aluxen-2026-09-21.dump   # restauração
```

Atenção: `--clean` derruba os objetos do destino. Faça a restauração com a
instância parada.

### Backup pelo produto (independente de dialeto)

O painel exporta e importa os cadastros em JSON (`GET /export`, `POST /import`,
apenas administrador). Serve para levar dados entre instâncias ou para um backup
pontual sem mexer no volume — **não** substitui o backup do banco (a fila, os
comprovantes lidos e a trilha de auditoria ficam de fora do arquivo).

---

## 6. Variáveis de ambiente por papel

Todas vêm do ambiente (12-factor). O contrato das chaves está em `.env.example`;
nenhum segredo tem padrão embutido no código.

### Obrigatórias em produção

| Variável | Observação |
|---|---|
| `APP_ENV=production` | Liga o modo produção (cookie `Secure`, exigência de segredo). |
| `SESSION_SECRET` | **Mínimo de 16 caracteres.** Assina sessão, CSRF e os tokens dos documentos. Em produção o boot **falha** sem ele. |

### Banco

| Variável | Observação |
|---|---|
| `DATABASE_URL` | `file:/data/aluxen.db` → SQLite; `postgres://...` → Postgres. Vazio cai para `DB_DIR`. |
| `DB_DIR` | Diretório do SQLite quando `DATABASE_URL` está vazia. Nas instâncias provisionadas: `/data`. |
| `STORAGE_DIR` | Anexos (vistoria/documentos). Vazio usa `<DB_DIR>/storage`. |

### Servidor e log

| Variável | Padrão | Observação |
|---|---|---|
| `HOST` | `0.0.0.0` | Endereço de escuta. |
| `PORT` | `3000` | Porta do app (a mesma do domínio no painel). |
| `TZ` | — | `America/Sao_Paulo` nas instâncias. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`, `silent`. |
| `LOG_FORMAT` | — | `json` (o padrão em produção). |
| `COOKIE_SECURE` | `false` | Em produção o cookie já é `Secure`; só force fora de produção. |

### URL pública e proteção do login

| Variável | Observação |
|---|---|
| `APP_URL` / `BASE_URL` | Base dos links de validação e dos QR codes. `APP_URL` vence. O provisionamento grava `https://<domain>`. |
| `RATE_LIMIT_MAX`, `RATE_LIMIT_GLOBAL_MAX`, `LOGIN_LOCK_THRESHOLD` | Limites de tentativa de login e bloqueio progressivo. |

### Semente do administrador

| Variável | Observação |
|---|---|
| `SEED_USERNAME`, `SEED_PASSWORD` | Administrador criado no primeiro boot. |
| `DEFAULT_LANDLORD_NAME`, `DEFAULT_LANDLORD_CPF` | Dados do locador gravados nas configurações quando ainda não existem. |
| `SEED_DEMO` | `true` popula a base com os dados de demonstração **a cada boot**. Só em demo. |

### Só de IA (leitura assistida)

| Variável | Observação |
|---|---|
| `AI_API_URL`, `AI_API_KEY` | Provedor de leitura. Sem elas a instância não lê documento nem comprovante. |
| `AI_MODEL` | Modelo usado nas extrações. |
| `AI_CONFIDENCE_THRESHOLD` | Confiança mínima para a baixa automática (padrão `0.9`). Abaixo disso, revisão humana. |

### Só de WhatsApp

| Variável | Observação |
|---|---|
| `WHATSAPP_MODE` | `demo` → WAHA (não oficial, risco de banimento: **apenas demonstração**); `client` → Cloud API oficial da Meta (cliente pagante). |
| `WAHA_API_URL`, `WAHA_API_KEY`, `WAHA_SESSION` | Conexão com o WAHA. |
| `WAHA_WEBHOOK_SECRET` | Valida a assinatura dos webhooks recebidos. |

### Provisionamento (ambiente do operador, não da instância)

| Variável | Observação |
|---|---|
| `EASYPANEL_URL`, `EASYPANEL_API_KEY` | Base e token da API do painel. **Só o operador precisa delas.** |
| `EASYPANEL_PROJECT` | Projeto do painel (padrão `aluxen`). |
| `SEED_USERNAME`, `SEED_PASSWORD`, `SESSION_SECRET` | Lidas pelo `provision create` quando a flag correspondente não é passada. |

### Demo × cliente

| | Demo | Cliente |
|---|---|---|
| `SEED_DEMO` | `true` | `false` |
| `WHATSAPP_MODE` | `demo` (WAHA) | `client` (API oficial) |
| Etiqueta `expires_at` | sempre | nunca |
| Dados | fictícios | reais |

---

## 7. Diagnóstico

### A instância não sobe

Comece pelo **log JSON do serviço** no painel. As mensagens úteis, na ordem em que
aparecem:

| Sintoma | O que significa |
|---|---|
| `falha ao iniciar: configuração de ambiente inválida: SESSION_SECRET deve ter ao menos 16 caracteres em produção` | `APP_ENV=production` sem segredo longo o bastante. O boot é *fail-closed* de propósito. |
| `falha ao iniciar: abrir banco: ...` | `DATABASE_URL`/`DB_DIR` errados, volume não montado ou Postgres inacessível. |
| `falha ao iniciar: aplicar migrações de ...` | Migração não aplicou. Veja a mensagem do SQL logo abaixo. |
| `falha ao escutar` + `err=... address already in use` | Porta ocupada — normalmente duas instâncias com o mesmo `PORT` no mesmo host. |
| `servidor iniciado` | Boot completo. A linha traz `dialect`, `database`, `version` e `env`. |

Depois do boot, a saúde é por HTTP:

```sh
curl -fsS https://<instância>/healthz   # liveness: "ok", não toca o banco
curl -fsS https://<instância>/readyz    # readiness: "ready"; 503 = banco indisponível
curl -fsS https://<instância>/api/health
```

- `/healthz` **200** e `/readyz` **503** → o processo está vivo e o banco não
  responde (log: `banco indisponível na readiness`). Confira o volume/DSN.
- Ambos **200** mas a página não abre → o problema é domínio/DNS/TLS, não o app
  (confira o registro DNS e o certificado no painel).

### Fila de trabalho (`jobs`)

Leitura de comprovante, leitura de documento, envio e cobrança rodam em uma fila
persistida na tabela `jobs` — ela sobrevive ao reinício do processo. O worker varre
a fila a cada 30s, pega até 20 trabalhos por varredura e tenta de novo em falha
transitória com espera exponencial (30s, 1min, 2min… até 30min), no máximo 3
tentativas por trabalho.

Estados: `queued` (na fila; `scheduled_at` no futuro = aguardando nova tentativa),
`running` (em execução), `done` (concluído), `failed` (tentativas esgotadas).

```sh
# SQLite
sqlite3 "$VOLUME/aluxen.db" \
  "select id, kind, status, attempts, scheduled_at, finished_at, substr(coalesce(error,''),1,80)
   from jobs order by created_at desc limit 20;"

# Postgres
psql "$DATABASE_URL" -c \
  "select id, kind, status, attempts, scheduled_at, finished_at, left(coalesce(error,''),80)
   from jobs order by created_at desc limit 20;"
```

O que olhar:

- **`running` parado por muito tempo** — o processo caiu no meio. No próximo boot o
  app devolve os órfãos para `queued` (log: `jobs órfãos devolvidos para a fila`,
  `count=N`).
- **`failed`** — o campo `error` diz o motivo. Trabalho falho pode ser reenfileirado
  com as tentativas zeradas pelo caso de uso de retentativa (`application.RetryJob`);
  trabalho em execução ou já concluído **não** é reexecutado (a baixa de um
  lançamento não pode acontecer duas vezes).
- **`queued` sem sair do lugar** — confira se o `kind` tem handler registrado no
  boot. Hoje o app registra `payment-proof` e `document-intake`; um trabalho de
  outro tipo falha com `nenhum handler registrado para o tipo "..."` e vai para
  `failed` — é sinal de que a área ainda não está ligada, não de banco quebrado.

No log do app:

| Mensagem | Significa |
|---|---|
| `fila: trabalho concluído` (`job`, `kind`, `attempts`) | Trabalho terminou. |
| `fila: trabalho adiado para nova tentativa` | Falha transitória; já tem novo horário. |
| `fila: trabalho órfão devolvido à fila` | Recuperação no boot. |
| `fila: varredura falhou` | Não foi possível ler a fila (banco). |
| `worker não encerrou no tempo esperado` | O encerramento esperou 20s pelo trabalho em execução. |

### Trilha de auditoria

Toda operação sensível (baixa, criação de cadastro, reajuste, exportação de backup,
decisão sobre comprovante) grava um evento em `audit_logs`, visível no painel na aba
de auditoria e exportável em CSV (`GET /audit/export`). É o primeiro lugar para
responder "quem fez isso e quando".
