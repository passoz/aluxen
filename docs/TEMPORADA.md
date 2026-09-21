# 🏖️ Manual de Locação de Temporada no Aluxen (Go)

> **Guia completo do subsistema de temporada do Aluxen.**
> Cobre: fundamentos da locação de temporada, ciclo de vida da caução, protocolo de vistoria fotográfica, comparação inteligente de vistorias por visão computacional (Tier 2), emissão do laudo técnico com prova pública e calendário de ocupação.

---

## 📑 Sumário

1. [Visão Geral & Enquadramento Legal](#1-visão-geral--enquadramento-legal)
2. [Gestão de Caução (Depósitos)](#2-gestão-de-caução-depósitos)
3. [Vistorias de Entrada e Saída (Check-in & Check-out)](#3-vistorias-de-entrada-e-saída-check-in--check-out)
4. [Comparador de Vistorias por Visão Computacional (Tier 2)](#4-comparador-de-vistorias-por-visão-computacional-tier-2)
5. [O Laudo Técnico de Comparação em PDF & Autenticidade](#5-o-laudo-técnico-de-comparação-em-pdf--autenticidade)
6. [Calendário de Ocupação & Prevenção de Conflitos](#6-calendário-de-ocupação--prevenção-de-conflitos)
7. [Categorias Específicas de Lançamento](#7-categorias-específicas-de-lançamento)

---

## 1. Visão Geral & Enquadramento Legal

O Aluxen atende a locação residencial convencional de longo prazo e a **locação de temporada** (*short-term / vacation rental*), disciplinada pelos artigos 48 a 50 da **Lei do Inquilinato (Lei nº 8.245/1991)**.

### Diferenças Estruturais

| Aspecto | Locação Residencial | Locação de Temporada |
|---|---|---|
| **Prazo Legal** | Tradicionalmente $\ge$ 12 a 30 meses | **Máximo de 90 dias** (art. 48) |
| **Garantia** | Caução ou fiador tradicional | **Caução à vista** exigida no check-in |
| **Recebimento** | Mensalidades sucessivas vencidas | Pagamento antecipado de diárias e taxas |
| **Vistoria** | Vistoria inicial e final espaçadas | **Vistorias imediatas** em cada rotatividade |
| **Taxas Extras** | Condomínio e IPTU ordinários | **Taxa de limpeza** e **consumíveis** |

> **Princípio de Não-Interferência:** As funcionalidades de temporada são estritamente **aditivas**. O uso tradicional do Aluxen (5 unidades residenciais, geração automática de recibo, baixa e exportação) continua 100% idêntico. Os campos e abas de temporada só operam quando o contrato tem status `season`.

---

## 2. Gestão de Caução (Depósitos)

Em contratos de temporada, o locador retém um valor de caução em garantia contra eventuais avarias ou consumo extraordinário.

### Estados da Caução (`deposit_status`)

```
               [Contrato Criado]
                       │
                       ▼
                    [none]  (sem caução retida)
                       │
                       │ (intent: deposit.set)
                       ▼
                    [held]  (caução retida)
                       │
         ┌─────────────┴─────────────┐
         │                           │
(intent: deposit.refund)    (intent: deposit.refund)
[valor total devolvido]     [valor parcial devolvido]
         ▼                           ▼
    [refunded]             [partially_refunded]
                                     │
                             [saldo restante]
                                     ▼
                                [forfeited]  (apropriação por dano)
```

1. **`none`**: Situação inicial quando não há caução registrada.
2. **`held`**: Caução retida integralmente. O locador informa o valor recebido, data de recebimento e observações via `POST /dashboard/deposit` (`intent=deposit.set`).
3. **`partially_refunded`**: O locador devolveu parte da caução (por exemplo, abatendo taxa de lavanderia ou taxa de limpeza extra).
4. **`refunded`**: Caução devolvida integralmente ao locatário no encerramento da estadia.
5. **`forfeited`**: Caução totalmente retida pelo locador devido a danos constatados.

### Emissão do Recibo de Devolução (`/deposit-pdf`)
Ao concluir a devolução parcial ou total da caução, o sistema emite o **Recibo de Devolução de Caução** em PDF vetorial (`GET /deposit-pdf?id=&token=`), contendo:
- Dados do locatário e do imóvel.
- Valor originalmente retido e valor efetivamente devolvido.
- Discriminação de deduções (se houver).
- Selo e QR Code público de autenticidade (`/validar/caucao/{id}`).

---

## 3. Vistorias de Entrada e Saída (Check-in & Check-out)

O Aluxen oferece controle detalhado do estado do imóvel através de autos de vistoria fotográficos (`POST /dashboard/inspection`).

### Especificações Técnicas da Vistoria
- **Tipos de Vistoria:** `checkin` (entrada do hóspede) e `checkout` (saída do hóspede).
- **Estado Geral:** `otima`, `boa`, `regular` ou `ruim`.
- **Itens Paralelos:** Área (ex.: *Sala*, *Cozinha*), Item (ex.: *Sofá*, *Geladeira*), Estado e Observações.
- **Galeria de Fotos:**
  - Até **10 fotos por vistoria**.
  - Tamanho máximo de **8 MiB por foto** (teto de 40 MiB por requisição).
  - Formatos aceitos: `image/jpeg` (JPG), `image/png` (PNG) e `image/webp` (WEBP).
  - Armazenamento em `/data/storage/inspections/` com hash para evitar sobreposição de nomes de arquivo.

### Auto de Vistoria em PDF (`/inspection-pdf`)
Gera documento PDF formatado com os itens conferidos e as miniaturas das fotos anexadas, permitindo validação pública instantânea por QR Code (`/validar/vistoria/{id}`).

---

## 4. Comparador de Vistorias por Visão Computacional (Tier 2)

A comparação manual de fotos entre check-in e check-out é lenta e propensa a disputas. O Aluxen resolve isso com uma **pipeline de IA multimodal acoplada a validação determinística por código**.

```
   [Fotos Check-in]                [Fotos Check-out]
          │                               │
          └───────────────┬───────────────┘
                          │
                          ▼
            [POST /api/inspections/compare]
                          │
                          ▼
          +───────────────────────────────+
          |  ai.Client (Multimodal Vision)|
          |  Prompt com vocabulário fixo  |
          +───────────────┬───────────────+
                          │
                          ▼
          +───────────────────────────────+
          | Resposta do Modelo (JSON)     |
          | - findings (achados por item) |
          | - severity (ok|atencao|dano)  |
          | - confidence (0.0 a 1.0)      |
          | - summary (resumo em texto)   |
          +───────────────┬───────────────+
                          │
                          ▼
+──────────────────────────────────────────────────────────────────+
| Validação Determinística da Aplicação (CompareLeaseInspections): |
| 1. Severidade fora do vocabulário ou JSON inválido -> 502 Bad GW |
| 2. Achado cita foto inexistente no upload? -> DESCARTADO         |
| 3. Achado sem nenhuma foto vinculada? -> DESCARTADO              |
| 4. Incrementa summary.Discarded com os achados ignorados         |
| 5. Emite laudo com AUTH-XXXX e aviso legal de subsídio           |
+─────────────────────────────────┬────────────────────────────────+
                                  │
                                  ▼
                    [GET /comparison-pdf?lease_id=]
```

### Regras Estritas de Engenharia e Defesa

1. **Separação por Camadas:**
   - O **Adaptador de IA (`internal/infrastructure/ai/compare.go`)** valida exclusivamente o **formato estrutural**: sintaxe JSON, envelope da resposta, tipos de dados e pertinência ao vocabulário de severidade (`ok`, `atencao`, `dano`).
   - O **Caso de Uso (`internal/application/inspection_comparison.go`)** aplica as **regras de negócio**: verifica se as fotos citadas realmente foram enviadas para aquela série (entrada ou saída). Se o modelo inventar uma referência a uma foto inexistente, o achado individual é **descartado** e a contagem `summary.Discarded` é incrementada, sem abortar a emissão do laudo.
2. **Resiliência contra Falhas de IA:**
   - Se a IA não estiver configurada (`AI_API_URL` vazio), a rota responde `503 Service Unavailable` com mensagem clara para o operador.
   - Se a IA devolver vocabulário não reconhecido ou alucinar fora do esquema, o sistema responde `502 Bad Gateway` com o texto amigável: *"A comparação devolveu um resultado fora do formato esperado. Tente novamente."* — **nunca gera cobrança indevida ou dano presumido**.

---

## 5. O Laudo Técnico de Comparação em PDF & Autenticidade

O resultado da comparação pode ser exportado em PDF vetorial A4 de alta qualidade através de `GET /comparison-pdf?lease_id=&token=`.

### Estrutura do Laudo
- **Cabeçalho:** Identificação da instância, do locador e do contrato de temporada.
- **Quadro Comparativo:** Identificação da vistoria de entrada e da vistoria de saída com datas, horários e fotos de cada lado.
- **Tabela de Achados Técnicos:**
  - Área e Item (ex: *Cozinha / Geladeira*).
  - Grau de Severidade:
    - 🟢 **Sem alteração (`ok`)**: Item em conformidade.
    - 🟡 **Atenção (`atencao`)**: Pequenas marcas de uso ou divergência sutil.
    - 🔴 **Dano (`dano`)**: Avaria material ou ausência de item presente na entrada.
  - Observação técnica e fotos comprobatórias vinculadas.
- **Resumo Quantitativo:** Total de itens conferidos, fotos analisadas, achados descartados por falta de sustentação e índice de confiança global da leitura.
- **Aviso Legal Obrigatório (Disclaimer):**
  > *"Este laudo constitui subsídio informativo e técnico para apoio à conferência de devolução do imóvel entre locador e locatário, não configurando perícia judicial nem imputação automática de débito."*
- **Selo Criptográfico:** Código `AUTH-XXXX-XXXX-XXXX-XXXX` e link de validação pública (`/validar/vistoria/{checkin_id}?token=...`).

---

## 6. Calendário de Ocupação & Prevenção de Conflitos

A aba **Calendário** do painel administrativo (`/dashboard?tab=calendar`) centraliza a ocupação dos imóveis:

### Características da Grade
- **Visualização Mensal por Imóvel:** Lista cada propriedade nas linhas e os dias do mês (1 a 28/30/31) nas colunas.
- **Destaque Visual:**
  - 🟣 **Temporada (`season`)**: Período ocupado por estadia curta.
  - 🔵 **Residencial Ativo (`active`)**: Período coberto por contrato residencial vigente.
  - ⚪ **Livre**: Dias sem locação aberta, disponíveis para novas reservas.
- **Prevenção Estrita de Overlap:**
  - Ao criar ou editar um contrato (residencial ou temporada), a aplicação executa a validação pura `Overlapping` no banco.
  - Se houver colisão de datas no mesmo imóvel, a operação é rejeitada com código `400` e mensagem informativa.

---

## 7. Categorias Específicas de Lançamento

Além de `rent` (aluguel padrão) e `extra` (avulso residencial), os contratos de temporada habilitam categorias financeiras especializadas:

1. **Taxa de Limpeza (`cleaning`):**
   - Lançamento específico cobrado do hóspede para higienização e lavanderia na rotatividade.
2. **Consumíveis (`consumables`):**
   - Cobrança de reposição de itens de consumo (ex: cápsulas de café, lenha, gás extra).
3. **Devolução de Caução (`deposit-refund`):**
   - Lançamento contábil que registra o estorno financeiro de caução devolvida ao locatário.
