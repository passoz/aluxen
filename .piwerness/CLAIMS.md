# Coordenação entre agentes — Aluxen (port Go)

Dois (ou mais) agentes trabalham no mesmo repositório. Este arquivo é o contrato de
coordenação: quem está com qual parte, quais arquivos são compartilhados, como pedir e como
integrar. **Leia antes de escrever código.**

## 1. Base comum

- Branch de trabalho: `devgo` — **`0d24895`** ("temporada ... e base da IA").
- Qualquer agente parte desta base (ou de um descendente dela) antes de escrever.

## 2. Territórios (não invadir)

| Árvore | Branch | Dono |
|---|---|---|
| `/home/passoz/dev/aluxen` (principal) | `devgo` | agente A |
| `/home/passoz/dev/aluxen-w0003` (worktree) | `work/0003-ai-entrada` | agente B |

**Regras de git que evitam perder trabalho do outro:**

1. Na árvore principal, **nunca** rode `git reset --hard`, `git checkout -- .`, `git clean -fd`,
   `git stash` sem `-k` nem `git restore .` — a árvore tem trabalho não commitado do outro agente.
2. **Nunca** use `git add -A`/`git add .` na árvore principal: sempre liste os caminhos que você
   mesmo editou (`git add internal/... queries/...`). Idem para `git commit -a`.
3. Não troque de branch na árvore principal enquanto houver mudanças não commitadas.
4. Quem quiser isolamento total cria worktree: `git worktree add ../aluxen-wNNNN -b work/NNNN devgo`
   (o worktree de outra pessoa não se remove sem avisar).

## 3. Frentes e donos

| Work | Escopo | Dono | Estado |
|---|---|---|---|
| 0001 | Port completo (fundação, painel, cadastros, documentos, portal, WhatsApp) | A | em andamento |
| 0002 | Temporada: caução, vistoria, calendário, taxas | A | base entregue em `0d24895` |
| 0003 | IA de entrada: comprovante → baixa automática; onboarding por documento; fila | B | em andamento |
| 0004 | WhatsApp bidirecional e cobrança | A | planejado |
| 0005 | (spec em `.piwerness/specs/0005.json`) | A | planejado |

### Protocolo de claim

1. Antes de começar uma task, escreva a linha na tabela acima (nesta seção) com seu nome e estado.
2. Ao terminar, marque `done` também em `.piwerness/work/NNNN/plan.json` (`tasks[].status`).
3. Precisa de um arquivo que está com o outro? **Peça** (seção 8) em vez de editar.

## 4. Work 0003 — divisão fina

Já entregue na base `0d24895` pelo agente A (não mexer sem avisar):

- `internal/domain/payments.go` — `Job`, `PaymentProof`, `DocumentIntake`, `EvaluatePaymentMatch`.
- `schema/{sqlite,postgres}.sql` + `migrations/*/00004_ai_intake.sql` — tabelas `jobs`,
  `payment_proofs`, `document_intakes` (e `inspections`, de 00002).
- `queries/queries.sql` — queries das três tabelas.
- `internal/infrastructure/storage/filesystem.go` — guarda de anexos.

Em execução pelo agente B (branch `work/0003-ai-entrada`):

| Item | Arquivos | Estado |
|---|---|---|
| 3.1 adapters + wiring + testes | `internal/infrastructure/db/{sqlite,postgres}/{jobs,payment_proofs,document_intakes}.go`, `db/store.go`, `db/store_test.go` | entregue (`59bff20`) |
| 3.2 cliente de IA | `internal/infrastructure/ai/**` | em andamento |
| 3.3 baixa automática | `internal/application/payment_proof.go` | pendente |
| 3.4 fila de revisão + confirmação | `internal/application/payment_review.go`, `web/handlers_payments.go`, templates | pendente |
| 3.5 onboarding por documento | `internal/application/document_intake.go`, `web/handlers_intake.go`, templates | pendente |
| 3.6 worker da fila | `internal/application/jobs.go`, `cmd/app/main.go` | pendente |

**Stubs que o agente B vai eliminar** (o andaime `handlers_scaffold.go` diz que nenhum pode
sobreviver ao fim do Work): `handlers_payments.go` (3), `handlers_intake.go` (2). O helper
`notImplemented` só sai quando não houver mais nenhum uso.

## 5. Arquivos compartilhados (cuidado redobrado)

- `queries/queries.sql` + `internal/infrastructure/db/{sqlite,postgres}/sqlcgen/**`:
  **quem edita query regenera os dois dialetos** —
  `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate -f sqlc.sqlite.yaml` e
  `... -f sqlc.postgres.yaml`. O teste `internal/infrastructure/db/parity` bloqueia divergência
  entre dialetos, entre schema e migrações e entre código gerado.
- `internal/infrastructure/db/store.go` e `store_test.go`: o agente B acrescentou `Jobs`, `Proofs`,
  `Intakes` e os testes `TestJobsRepository`, `TestPaymentProofsRepository`,
  `TestDocumentIntakesRepository`.
- `internal/domain/payments.go` / `UpdatePaymentProofDecision`: alteração do agente B — a query
  passou a persistir também a leitura (`extracted_*`, `confidence`, `raw_response`), porque a
  extração é gravada no mesmo passo da decisão. Sem isso a leitura do comprovante se perdia.
- `internal/application/deps.go`, `internal/infrastructure/web/common.go`, `config/config.go`:
  pontos de injeção. Mudança aqui afeta os dois — avise antes.
- **Migrações**: `00001`, `00002`, `00003` (temporada) e `00004` (IA) estão tomadas. A próxima
  livre é `00005`; toda migração nasce nos **dois dialetos** e com `IF NOT EXISTS`.
- `internal/infrastructure/web/router.go`: as rotas de 0003 já existem (linhas 55-60) — implemente
  o handler, não crie rota nova sem combinar.

## 6. Gates antes de commitar (qualquer agente)

```bash
go build ./... && go vet ./... && go test ./...
# se mexeu em query: regenere o sqlc nos dois dialetos (seção 5)
# se mexeu em schema/migração: go test ./internal/infrastructure/db/parity/
```

Governança (piwerness) do Work 0003: `pwn work gate <GATE> --work 0003`,
`pwn work run --work 0003 -- go test ./...` (roda os 5 gates + sandbox + diff guard) e
`pwn work audit --work 0003 --task 3.x`. Binário: `bun /home/passoz/dev/piwerness/bin/pwn.js`.

## 7. Como integrar

```bash
# na árvore principal (devgo), com a árvore limpa:
git merge work/0003-ai-entrada     # fast-forward se ninguém tocou os mesmos arquivos
```

Conflito em `queries.sql`, `store.go` ou `store_test.go`: mantenha **as duas adições** (as frentes
são aditivas por contrato) e **regenera o sqlc** antes de commitar. Conflito em `payments.go`:
fale com o dono do 0003.

## 8. Canal de mensagens (sem RPC entre sessões)

As sessões `omp` são processos distintos e o `hub` de uma **não** enxerga a outra. O canal é este
arquivo + o humano como ponte:

- **Pedidos pendentes** — escreva aqui, com data, o que precisa e de quem:
  - _(vazio)_
- **Mensagens** — mesma coisa, em uma linha por assunto:
  - _(vazio)_

Regra: mensagem pede algo ou responde algo. Ao resolver, apague a linha (ou marque `RESOLVIDO`).

## 9. Convenções que não se negociam

- `domain` não importa `application`/`infrastructure`; `infrastructure` importa `domain`.
- Nenhum SQL montado em runtime; nenhum handler com regra de negócio (ver `docs/ARQUITETURA-GO.md`).
- Toda mudança é **aditiva**: o uso atual (contrato residencial, recibo mensal, baixa) continua idêntico.
- Código, endpoints e nomes de arquivo em **inglês**; texto de UI, comentários e mensagens de erro
  em **português**.
- Portas do domínio são congeladas: para acrescentar método, combine com o outro agente.
