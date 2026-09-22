# Casos de uso do Laya SDK no Aluxen

> Branch `devgo` · documento de análise — nenhuma linha de código do app foi
> alterada. Este arquivo mapeia, com exemplos, todos os pontos em que o
> [`laya-sdk`](https://github.com/passoz/laya-sdk) (modelo System One de decisão)
> faz sentido, como ele se encaixa na arquitetura vigente e o que ele **não**
> deve substituir.

---

## 1. Por que o Laya cabe no Aluxen

O Aluxen já tem uma regra de ouro repetida em todo o código:

> **O modelo lê, o código decide.** — `internal/application/agent.go`,
> `internal/domain/payments.go` (`EvaluatePaymentMatch`), `internal/application/collection.go`.

Hoje o único "cérebro" é um LLM em nuvem (DeepSeek, protocolo OpenAI em
`internal/infrastructure/ai/client.go`), usado em três lugares: leitura de
comprovante (`ProofExtractor`), leitura de documento de cadastro
(`IntakeExtractor`) e conversa com ferramentas (`ChatClient`). Toda chamada de
LLM tem latência e custo, e o app já investe muito em **decisões
determinísticas** para não depender dele.

O **Laya** é a peça que faltava no meio: um modelo de decisão **local**
(`ghcr.io/passoz/laya`, pesos Apache-2.0), que devolve probabilidades tipadas
(`choice`, `noul`, `score`) em **~20–30 ms por chamada**, sem gerar texto, sem
custar por token e **sem dados saindo da máquina** — alinhado ao BR-202
(privacidade: dado pessoal não sai do processo). É o "System 1" barato que roda
na frente do LLM (padrão *cascade*): decide o fácil, escala o difícil.

### Princípios (inabaláveis, coerentes com o código atual)

1. **Laya nunca gera a resposta.** Ele devolve probabilidades; quem redige é
   código determinístico — como já fazem `agentEscalationReply` e
   `CollectionMessage`.
2. **Laya nunca substitui regra determinística.** `EvaluatePaymentMatch`,
   `CollectionStageByDelay` e `legalQuestion` continuam soberanos. O Laya é uma
   camada **anterior** a elas, que decide *se* e *para onde* rotear — nunca o
   veredito final.
3. **Nenhum dado sai da máquina.** O Laya roda como sidecar local
   (`127.0.0.1`), ao contrário do LLM de visão/chat, que é nuvem.
4. **Abaixo do threshold, escala.** Decisão de baixa confiança nunca age
   sozinha; cai no caminho que já existe (LLM ou humano), exatamente como o
   `MinConfidence` da baixa de comprovante.

---

## 2. Visão geral dos casos de uso

| # | Caso de uso | Onde pluga hoje | Tipo de decisão | Ganho principal |
|---|---|---|---|---|
| UC-1 | Pré-triagem da mensagem recebida | `agent.go` → `HandleIncomingMessage` | `choice` + `noul` | corta LLM nas mensagens simples |
| UC-2 | Detecção jurídica (robustecer `legalQuestion`) | `agent.go` → `legalQuestion`/`legalTerms` | `noul` | pega paráfrase que a lista de termos perde |
| UC-3 | Priorização da fila de conversas | `agent.go` → `ListConversations` | `score` + `noul` | caixa de entrada ordenada por risco, não por hora |
| UC-4 | Triagem das respostas à cobrança | `agent.go` (mensagem recebida) + `collection.go` | `choice` + `noul` | roteia réplica à régua sem LLM |
| UC-5 | Filtro de documento antes da visão | `whatsapp.go`/`documents.go` → `ProofExtractor` | `choice` + `noul` | descarta lixo barato antes da chamada de visão |
| UC-6 | Classificação de documento no intake | `document_intake.go` → `SubmitDocumentIntake` | `choice` | pré-preenche `IntakeKind` e reduz erro humano |
| UC-7 | Roteamento de consulta do relatório | `ask.go` | `choice` | responde consulta comum sem LLM |
| UC-8 | Moderação do canal (spam/fraude/ofensa) | webhook WhatsApp (`whatsapp.go`) | `noul` | guardrail antes de qualquer resposta automática |
| UC-9 | Priorização de achados de vistoria | `inspection_comparison.go` | `score` + `noul` | ordena o laudo para conferência do locador |
| UC-10 | Score de churn / saúde da carteira | leitura de histórico (`ListConversations`) | `score` + `noul` | alarme de locatário em risco |

---

## 3. Casos de uso em detalhe

### UC-1 — Pré-triagem da mensagem recebida (o caso central)

**Onde:** `internal/application/agent.go`, função `HandleIncomingMessage`.
Hoje **toda** mensagem entra no laço de ferramentas do LLM (`agentMaxRounds = 4`)
e só escala quando o provedor não encontra ferramenta (`agentReasonUngrounded`)
ou quando o texto bate em `legalTerms`.

**Proposta:** antes de montar o pedido do agente, uma única chamada local do
Laya decide a intenção e se vale a pena acionar o LLM. Mensagem trivial é
resolvida por roteamento direto à ferramenta tipada; o resto segue o fluxo
atual.

```go
import (
    "context"
    laya "github.com/passoz/laya-sdk"
)

// triage roda uma única passada do Laya e devolve a decisão tipada.
type triageDecision struct {
    Intent        string  // payment | maintenance | contract | legal | out_of_scope
    Legal         float64 // P(true)
    RequiresHuman float64 // P(true)
    Urgent        float64 // P(true)
    IntentConf    float64 // confiança da intenção
}

func triage(ctx context.Context, c *laya.Client, body string) (triageDecision, error) {
    questions := laya.NewQuestions().
        Choice("intent", "Qual é a intenção principal da mensagem do locatário?", map[string]string{
            "payment":      "pagamentos, boletos, recibos e comprovantes",
            "maintenance":  "reparos, infiltração, defeitos e pedidos de conserto",
            "contract":     "renovação, multa, reajuste, cláusulas e termos",
            "legal":        "ameaça de processo, advogado, ação judicial, rescisão",
            "out_of_scope": "elogios, saudações e assuntos fora do atendimento",
        }).
        Noul("legal", "A mensagem manifesta dúvida ou ameaça jurídica?", map[string]string{
            "true":  "menciona processo, advogado, justiça, indenização ou rescisão formal",
            "false": "não trata de questão jurídica",
        }).
        Noul("requires_human", "A mensagem exige intervenção humana (reclamação formal, jurídico ou fora do escopo das ferramentas)?", nil).
        Noul("urgent", "A mensagem comunica pressa, prazo ou emergência?", nil)

    res, err := c.Predict(ctx, body, questions, laya.WithLang("pt"))
    if err != nil {
        return triageDecision{}, err
    }
    intent, _ := res.Choice("intent")
    legal, _ := res.Noul("legal")
    requiresHuman, _ := res.Noul("requires_human")
    urgent, _ := res.Noul("urgent")
    conf, _ := res.Confidence("intent")
    return triageDecision{Intent: intent, Legal: legal, RequiresHuman: requiresHuman,
        Urgent: urgent, IntentConf: conf}, nil
}
```

**Decisão** (no início de `HandleIncomingMessage`, só quando `deps.Decider != nil`):

```go
d, err := triage(ctx, client, msg.Body)
if err == nil {
    switch {
    case d.Legal >= 0.8:
        // substitui o acerto por palavra-chave: escalonamento jurídico sem LLM.
        return escalateToHuman(ctx, deps, actx, msg, tenant, agentReasonLegal)
    case d.Intent == "maintenance" && d.IntentConf >= 0.85:
        // roteia direto para a ferramenta tipada, sem passar pelo laço do LLM.
        return runMaintenanceDirect(ctx, deps, actx, msg, tenant) // openMaintenanceRequest
    case d.Intent == "out_of_scope" || d.RequiresHuman >= 0.8:
        return escalateToHuman(ctx, deps, actx, msg, tenant, agentReasonUngrounded)
    }
    // abaixo do threshold: segue o laço de ferramentas do LLM, como hoje.
}
```

Ganho: mensagens do tipo *"o pagamento já caiu?"* ou *"o chuveiro quebrou"* —
a grande maioria — deixam de pagar uma ida ao LLM.

---

### UC-2 — Detecção jurídica por probabilidade (substitui a lista de termos)

**Onde:** `internal/application/agent.go` → `legalQuestion` e `legalTerms`.

A lista de termos é deliberadamente curta ("escalar é seguro, responder o que
não pode é que não é"), mas perde paráfrases: *"vou procurar meus direitos"*,
*"multa abusiva"*, *"me senti constrangido"*, *"quero rescindir por justa causa"*.

**Proposta:** manter `legalQuestion` como pré-filtro determinístico (barato e
inquestionável) e adicionar o Laya como segunda camada. Termo presente →
escala; senão, o Laya julga; abaixo do threshold, o LLM decide.

```go
// legalQuestion continua a primeira barreira (determinística).
if reason, ok := legalQuestion(msg.Body); ok {
    return escalateToHuman(ctx, deps, actx, msg, tenant, reason)
}

// Segunda barreira, semântica, local e barata.
if d, err := triage(ctx, client, msg.Body); err == nil && d.Legal >= 0.9 {
    return escalateToHuman(ctx, deps, actx, msg, tenant, agentReasonLegal)
}
```

Ganho: captura a ameaça jurídica reescrita com outras palavras, mantendo o
determinismo como primeira linha de defesa.

---

### UC-3 — Priorização da fila de conversas (aba Conversas)

**Onde:** `internal/application/agent.go` → `ListConversations` e `conversationRow`.

Hoje a fila é ordenada pela mensagem mais recente; `AwaitingReply` e
`Overdue` marcam pendências, mas um locatário furioso **em dia** não sobe.

**Proposta:** um `score` de urgência/frustração e um `noul` de risco de churn por
conversa, rodados em lote, para ordenar a caixa do locador.

```go
questions := laya.NewQuestions().
    Score("frustration", "Qual o grau de irritação do locatário?", "calmo", "levemente insatisfeito", "irritado", "furioso ou ameaçando").
    Noul("churn", "A mensagem sugere que o locatário pode cancelar ou sair?", nil)

res, _ := client.Predict(ctx, lastMessageBody, questions, laya.WithLang("pt"))
frustration, _ := res.Score("frustration")
churn, _ := res.Noul("churn")

// A linha da conversa ganha um peso de prioridade = frustração + bônus de churn.
```

Ganho: o locador atende primeiro quem está prestes a sair, não quem falou por
último. Nada aqui muda o que já existe — só a ordenação.

---

### UC-4 — Triagem das respostas à cobrança (régua reativa)

**Onde:** `internal/application/collection.go` (régua determinística) +
`agent.go` (mensagem recebida).

A régua envia o estágio certo pelo atraso (`CollectionFriendlyDays=1`,
`CollectionFirmDays=7`, `CollectionInstallmentDays=15`) — isso **não muda**. O
que falta é classificar a **réplica** do locatário à cobrança.

**Proposta:** classificar a resposta para rotear sem LLM:

```go
questions := laya.NewQuestions().
    Choice("reply", "Como classificar a resposta do locatário à cobrança?", map[string]string{
        "promise":        "promete pagar em breve, dá data ou confirma pagamento",
        "dispute":        "contesta valor, vencimento ou a própria cobrança",
        "hardship":       "relata dificuldade financeira e pede prazo ou parcelamento",
        "threat":         "ameaça processo, cancelamento ou reclamar em órgão de defesa",
        "other":          "nenhuma das anteriores",
    }).
    Noul("commitment", "O locatário se compromete a pagar?", nil)

res, _ := client.Predict(ctx, msg.Body, questions, laya.WithLang("pt"))
reply, _ := res.Choice("reply")

switch reply {
case "promise":
    registerPromise(ctx, deps, actx, tenant) // acompanha e confirma o combinado
case "dispute", "threat":
    escalateToHuman(ctx, deps, actx, msg, tenant, agentReasonUngrounded)
case "hardship":
    proposeInstallment(ctx, deps, actx, tenant) // liga ao estágio de parcelamento (15d)
}
```

Ganho: a régua reage à réplica em milissegundos, sem LLM, e o humano só entra
em disputa e ameaça.

---

### UC-5 — Filtro de documento antes da visão (comprovante)

**Onde:** `internal/application/whatsapp.go`/`documents.go` (upload) →
`ProofExtractor.ExtractPaymentProof` (visão em nuvem).

A chamada de visão é o passo caro. Um PDF qualquer, um print de conversa ou um
documento que não é comprovante desperdiça a chamada e enche a fila de revisão.

**Proposta:** classificar o **texto que acompanha o arquivo** (legenda/remetente)
antes de gastar visão. O Laya é textual; ele não lê a imagem — lê o contexto.

```go
questions := laya.NewQuestions().
    Choice("doc", "Que tipo de anexo o locatário enviou?", map[string]string{
        "receipt":   "comprovante de pagamento, Pix, boleto pago, transferência",
        "contract":  "contrato de locação",
        "id":        "documento de identidade, RG, CNH, CPF",
        "address":   "comprovante de residência",
        "other":     "foto do imóvel, print de conversa, arquivo avulso",
    }).
    Noul("usable", "O anexo parece ser um comprovante de pagamento legível?", nil)

res, _ := client.Predict(ctx, caption, questions, laya.WithLang("pt"))
if kind, _ := res.Choice("doc"); kind != "receipt" {
    return ErrInvalidDocument // descarta barato, sem chamar visão
}
```

Ganho: menos chamadas de visão, menos ruído na fila de revisão humana.

---

### UC-6 — Classificação automática de documento no intake

**Onde:** `internal/application/document_intake.go` → `SubmitDocumentIntake`
(`input.Kind` é escolhido pelo humano).

Hoje o locador informa o tipo (`lease`, `id-document`, `address-proof`). Um
erro aqui manda o documento para o extrator errado e a proposta nasce
preenchida com campos que não batem.

**Proposta:** pré-classificar o tipo pelo texto que acompanha o upload e
pré-preencher `IntakeKind`, com o humano confirmando (como já é o fluxo).

```go
questions := laya.NewQuestions().
    Choice("kind", "Que tipo de documento é este?", map[string]string{
        "lease":         "contrato de locação com prazo, valor e reajuste",
        "id-document":   "documento de identidade: RG, CNH ou CPF",
        "address-proof": "comprovante de residência: conta, correspondência, declaração",
    })

res, _ := client.Predict(ctx, fileName+" "+caption, questions, laya.WithLang("pt"))
if kind, ok := res.Choice("kind"); ok && domain.IntakeKind(kind).Valid() {
    input.Kind = kind // sugestão; a confirmação humana continua soberana
}
```

Ganho: menos proposta errada, menos retrabalho na conferência.

---

### UC-7 — Roteamento de consulta do relatório (`/api/report/ask`)

**Onde:** `internal/application/ask.go` (`askMaxRounds = 4`, ferramentas tipadas).

Consultas previsíveis (*"quanto recebi em março?"*, *"quem está inadimplente?"*)
não precisam do laço de ferramentas do LLM.

**Proposta:** classificar a pergunta e rotear direto para a ferramenta
correspondente nas consultas comuns; só as ambíguas vão ao LLM.

```go
questions := laya.NewQuestions().
    Choice("query", "Qual consulta o usuário quer?", map[string]string{
        "monthly_total": "total recebido ou lançado em um mês",
        "overdue":       "lista de lançamentos em atraso",
        "tenant_balance":"situação financeira de um locatário específico",
        "other":         "pergunta que não é desses três tipos",
    })

res, _ := client.Predict(ctx, question, questions, laya.WithLang("pt"))
if q, ok := res.Choice("query"); ok && q != "other" {
    return runReportToolDirect(ctx, deps, q, question) // executa a leitura determinística
}
// "other" ou baixa confiança → laço de ferramentas do LLM, como hoje.
```

Ganho: a pergunta mais comum sai da mesma leitura determinística do relatório,
sem LLM.

---

### UC-8 — Moderação do canal (spam, fraude, ofensa)

**Onde:** webhook de recebimento (`internal/application/whatsapp.go`) — antes de
qualquer resposta automática.

```go
questions := laya.NewQuestions().
    Noul("spam", "A mensagem é spam, fraude ou tentativa de golpe?", map[string]string{
        "true":  "oferta não solicitada, pedido de senha, link suspeito, golpe",
        "false": "mensagem legítima de locatário",
    }).
    Noul("abusive", "A mensagem contém ofensa, xingamento ou conteúdo abusivo?", nil)

res, _ := client.Predict(ctx, msg.Body, questions, laya.WithLang("pt"))
if spam, _ := res.Noul("spam"); spam >= 0.9 {
    return ignoreMessage(ctx, deps, actx, msg) // descarta sem responder
}
if abusive, _ := res.Noul("abusive"); abusive >= 0.9 {
    return escalateToHuman(ctx, deps, actx, msg, tenant, "conteúdo abusivo")
}
```

Ganho: um guardrail local e determinístico antes de o canal responder, sem
enviar o texto do locatário à nuvem.

---

### UC-9 — Priorização de achados de vistoria (pós-comparação)

**Onde:** `internal/application/inspection_comparison.go` (a comparação em si é
visão — o Laya não lê imagem). O Laya atua **sobre o texto dos achados** já
devolvidos.

```go
// para cada finding devolvido pela comparação de fotos:
questions := laya.NewQuestions().
    Noul("flag", "Este achado merece atenção imediata do locador (dano grave, segurança)?", map[string]string{
        "true":  "dano estrutural, risco de segurança ou prejuízo relevante",
        "false": "desgaste leve ou sem gravidade",
    })

for _, f := range comparison.Findings {
    res, _ := client.Predict(ctx, f.Note+" "+f.State, questions, laya.WithLang("pt"))
    if flag, _ := res.Noul("flag"); flag >= 0.85 {
        f.Priority = high // sobe no laudo que o locador confere
    }
}
```

Ganho: o laudo (que já é "subsídio para o locador") chega ordenado pelo que
importa, sem decidir dano ou retenção — isso continua humano.

---

### UC-10 — Score de churn / saúde da carteira

**Onde:** leitura de histórico em `ListConversations` / relatório.

Sinal de alerta para o locador: locatário que já foi atendido, não está em
atraso, mas demonstra insatisfação crescente.

```go
questions := laya.NewQuestions().
    Score("risk", "Qual o risco de o locatário encerrar o contrato?", "nenhum", "baixo", "médio", "alto").
    Noul("silent_risk", "O locatário está insatisfeito mesmo sem estar em atraso?", nil)

res, _ := client.Predict(ctx, history, questions, laya.WithLang("pt"))
risk, _ := res.Score("risk")
```

Ganho: visibilidade de churn que o critério puramente financeiro (atraso) não
captura.

---

## 4. Como plugar (proposta de integração)

O SDK é puro Go, sem dependência além da stdlib, e fala com o Laya por HTTP
local. Duas formas de subir:

### 4.1 Sidecar local (recomendado, coerente com a arquitetura)

O Laya roda como container ao lado do binário do Aluxen, em `127.0.0.1`:

```yaml
# docker-compose.yml (exemplo)
services:
  aluxen:
    image: ghcr.io/passoz/aluxen:latest
    environment:
      - LAYA_URL=http://laya:8770
      - LAYA_API_KEY=${LAYA_API_KEY}
  laya:
    image: ghcr.io/passoz/laya:latest
    environment:
      - HOST=0.0.0.0
      - API_KEY=${LAYA_API_KEY}
      - MODELS=multilingual   # pt-BR; english se o contrato estiver em outro idioma
    volumes:
      - laya-cache:/root/.cache/huggingface
```

### 4.2 Porta na camada de aplicação (proposta)

Seguindo o padrão já usado em `deps.go` (porta publicada pelo domínio,
implementação na infraestrutura), um novo port:

```go
// internal/application/deps.go (proposta)
type Decider interface {
    Triage(ctx context.Context, body string) (TriageDecision, error)
}
```

E a implementação em `internal/infrastructure/laya` embrulha o SDK:

```go
// internal/infrastructure/laya/decider.go (proposta)
package laya

import (
    "context"
    sdk "github.com/passoz/laya-sdk"
)

type Decider struct {
    client *sdk.Client
    lang   string
}

func New(baseURL, apiKey, lang string) *Decider {
    return &Decider{
        client: sdk.New(
            sdk.WithBaseURL(baseURL),
            sdk.WithAPIKey(apiKey),
        ),
        lang: lang,
    }
}

func (d *Decider) Triage(ctx context.Context, body string) (application.TriageDecision, error) {
    // uma chamada, quatro julgamentos em um único forward pass (UC-1)
}
```

### 4.3 Configuração (novas env vars, proposta)

| Variável | Padrão | Descrição |
|---|---|---|
| `LAYA_URL` | `http://127.0.0.1:8770` | Base do sidecar Laya |
| `LAYA_API_KEY` | vazio | Chave do sidecar (deixa o decider desligado se ausente) |
| `LAYA_LANG` | `pt` | Idioma das perguntas (pt → `multilingual`, en → `english`) |

Seguindo o padrão de `ErrNotConfigured` e `Options.AIEnabled`, o `Decider` nulo
desliga a triagem e o app volta a funcionar exatamente como hoje — o Laya é um
incremento opcional, nunca um requisito.

### 4.4 Concorrência e aquecimento

O Laya serve **um forward pass por vez** (GPU/CPU serial). O sidecar já
serializa com lock (`api_server.py`), mas o SDK não precisa de lock próprio se
chamado de um único worker. Importante: a primeira chamada compila kernels e é
várias vezes mais lenta — fazer **uma chamada de aquecimento no boot** com uma
pergunta representativa (o mesmo cuidado que o sidecar já documenta).

### 4.5 Calibração

O checkpoint `multilingual` (pt) **não é calibrado** (temperatura 1.0): ele
reporta 100%/0% com facilidade. Por isso os thresholds acima são altos (0.8–0.9)
e **nunca** devem ser lidos como certeza. Antes de confiar nos números, rodar o
conjunto rotulado de mensagens reais (o próprio benchmark em
`examples/realestate_bench` do SDK serve de molde) e ajustar thresholds por
pergunta — o mesmo rito que o Aluxen já adota com `AI_CONFIDENCE_THRESHOLD`
(padrão `0.9`).

---

## 5. Anti-padrões — onde o Laya **não** entra

| Não usar para | Por quê | Quem continua responsável |
|---|---|---|
| **Comparar fotos de vistoria** | Laya é textual; não lê imagem | `PhotoComparator` (visão em nuvem) |
| **Dar baixa em comprovante** | a regra é determinística e sagrada | `EvaluatePaymentMatch` |
| **Decidir o estágio da régua** | regra determinística por dias de atraso | `CollectionStageByDelay` |
| **Redigir mensagem ao locatário** | Laya não gera texto | `CollectionMessage`, `agentEscalationReply` |
| **Fazer aritmética** (valor, juros, datas) | encoder não lê números; o código faz a conta | `format`, `lateDays`, `amountsMatch` |
| **Extrair campo livre** (CPF, valor, endereço) | isso é extração, não decisão | `ProofExtractor`, `IntakeExtractor` (visão) |
| **`score` ordinal fino sem calibração** | o tipo mais fraco do modelo | calibrar ou manter o LLM para graduação sutil |

Regra de bolso, já escrita no código: **o modelo lê e classifica; o código
valida, decide e redige.** O Laya entra exatamente onde falta uma *decisão
semântica barata e local* — e fica fora de tudo que já é determinístico, envolve
imagem, aritmética ou texto livre.

---

## 6. Referências no código (branch `devgo`)

- Atendimento automático: `internal/application/agent.go`
  (`HandleIncomingMessage`, `ListConversations`, `legalQuestion`, `legalTerms`,
  `agentReasonLegal`, `agentReasonUngrounded`, `agentMaxRounds`).
- Ferramentas tipadas: `internal/application/agent_tools.go`
  (`openReceiptsOf`, `contractOf`, `receiptStatusOf`, `openMaintenanceRequest`,
  `escalateToLandlord`).
- Contrato do chat: `internal/application/chat.go` (`ChatClient`).
- Portas e dependências: `internal/application/deps.go`
  (`Proofs`, `Intakes`, `Chat`, `Photos`, `Options.AIEnabled`).
- Régua de cobrança: `internal/application/collection.go` e
  `internal/domain/collection.go` (`CollectionFriendlyDays=1`,
  `CollectionFirmDays=7`, `CollectionInstallmentDays=15`,
  `CollectionStageByDelay`).
- Baixa de comprovante: `internal/domain/payments.go`
  (`EvaluatePaymentMatch`, `PaymentMatchInput.MinConfidence`).
- Intake: `internal/application/document_intake.go`
  (`SubmitDocumentIntake`, `ProcessDocumentIntake`, `ConfirmDocumentIntake`,
  `extractIntakeDocument`) e `internal/domain/payments.go`
  (`IntakeKind`: `lease`, `id-document`, `address-proof`).
- Consulta NL: `internal/application/ask.go` (`askMaxRounds`,
  `AskRefusalMessage`).
- Comparação de vistorias: `internal/application/inspection_comparison.go`.
- Cliente do provedor de IA: `internal/infrastructure/ai/client.go`
  (`ErrNotConfigured`, `AI_API_URL`, `AI_API_KEY`, `AI_MODEL`) e
  `internal/infrastructure/config/config.go` (`AIConfidenceThreshold`).

> Este documento propõe **integração** (novo port + pacote de infraestrutura +
> env vars) e **não altera** nenhum arquivo existente. As assinaturas e os
> símbolos citados são os vigentes no branch `devgo`.
