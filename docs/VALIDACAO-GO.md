# Validação pública de documentos no Aluxen (Go)

Este documento descreve o protocolo de autenticidade e o portal público de
validação no port Go, e a dependência que ele cria: **o QR Code impresso nos
documentos só funciona enquanto o servidor da instância estiver no ar.**

---

## 1. Como funciona

Cada recibo e contrato impresso leva um **QR Code** e um **código legível**
(`AUTH-XXXX-XXXX-XXXX-XXXX`). O QR aponta para a página pública de validação:

- Recibos: `GET /validar/recibo/{id}?token=...`
- Contratos: `GET /validar/contrato/{id}?token=...`
- Autos de vistoria: `GET /validar/vistoria/{id}?token=...`
- Recibos de devolução de caução: `GET /validar/caucao/{id}?token=...`

O token é `HMAC-SHA256(SESSION_SECRET, "<tipo>:<id>")`, truncado nos 32 primeiros
caracteres hexadecimais (128 bits) — o **mesmo cálculo do app Bun**, de modo que
documentos já impressos continuam válidos quando a instância usa o mesmo
`SESSION_SECRET`. A verificação é em tempo constante.

Quem lê o QR cai na página pública, que confere o token contra o registro no banco
e mostra o selo de documento autêntico (ou o alerta de que a autenticidade não
pôde ser comprovada), com um botão para baixar o PDF original.

---

## 2. O QR aponta para a URL pública configurada

O endereço impresso no QR sai da **URL pública da instância**, resolvida nesta
ordem de prioridade:

1. Configuração `app_url` gravada na tela de Configurações.
2. Variável de ambiente `APP_URL` (vence `BASE_URL`).
3. `BASE_URL`.
4. Host da requisição que emitiu o documento, quando não é `localhost`.
5. Endereço IPv4 privado da máquina (rede local).
6. Endereço de produção padrão (`https://aluxen.evolucsia.com`).

Como o QR é lido **fora** da requisição que emitiu o documento — dias ou meses
depois, de outro aparelho —, o endereço precisa ser o **configurado na
instância**, não o host do momento. **Configure `app_url`/`APP_URL` com o domínio
público real** antes de emitir documentos.

---

## 3. A dependência de hospedagem contínua

> **O QR Code só funciona se o servidor estiver no ar.**

O QR Code não carrega os dados do documento: ele é um **ponteiro** para a página
pública de validação. A conferência acontece no servidor da instância, no momento
em que o QR é lido. Consequências diretas:

- **Servidor desligado, QR morto.** Sem o servidor respondendo no endereço do QR,
  a página não abre e o documento não pode ser validado — mesmo que o papel
  esteja íntegro.
- **Domínio ou URL pública errados** produzem QR que aponta para o lugar errado.
- **`SESSION_SECRET` trocado** invalida os tokens emitidos antes da troca: os QR
  dos documentos antigos deixam de conferir.

Isso é uma **escolha de operação**, não um defeito: a validação é sempre contra o
registro vivo no banco da instância, o que evita emitir no papel um dado que pode
ter mudado depois.

---

## 4. Modo de operação: instância hospedada centralizada

O Aluxen no Go roda como **uma instância hospedada por cliente** — um container
com volume próprio, servido continuamente (no Easypanel, com domínio e TLS). Não
é um programa de mesa que o locador liga só quando quer emitir um recibo.

Nesse modo:

- O servidor fica no ar 24/7 e o QR permanece válido.
- O domínio público é fixo e estável (`app_url`/`APP_URL`), então os QRs emitidos
  continuam apontando para o mesmo lugar.
- A disponibilidade do portal de validação acompanha a disponibilidade da
  instância: se o container cai, os QRs param de responder até ele voltar.

Instâncias de **demonstração** são provisionadas e removidas por script; ao
remover uma demo, os QRs dos documentos emitidos por ela deixam de responder.

---

## 5. Aviso que deve acompanhar o produto

Ao entregar o produto (documentação ao locador e à tela de validação), o aviso
mínimo é:

> **A validação do QR Code depende do servidor no ar.** Os QR Codes impressos nos
> recibos e contratos apontam para a URL pública configurada nesta instância. Se o
> servidor estiver desligado, o domínio mudar ou o `SESSION_SECRET` for trocado,
> os QR Codes deixam de conferir. Mantenha a instância hospedada e a URL pública
> estável para que os documentos impressos continuem validáveis.

---

## 6. Boas práticas

1. **Defina `SESSION_SECRET` forte e estável** (`openssl rand -hex 32`) e não o
   troque: a troca invalida todos os tokens já emitidos.
2. **Configure `app_url`/`APP_URL`** com o domínio público antes de emitir
   documentos; evite depender do host da requisição.
3. **Sirva sob HTTPS** para proteger o token em trânsito.
4. **Mantenha a instância no ar** — o portal de validação é um serviço contínuo.
