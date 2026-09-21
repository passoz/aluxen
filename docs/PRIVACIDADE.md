# Aviso de Privacidade — Aluxen

Este é o aviso de privacidade do **Aluxen**, o sistema de gestão de locações. Ele
explica, em linguagem direta, quais dados pessoais o sistema trata, para quê, por
quanto tempo, quem decide sobre eles e como o titular exerce os seus direitos.

---

## 1. Quem é quem

- **Controlador:** o **locador** — a pessoa física ou jurídica dona da instância do
  Aluxen. Cada instância é instalada e operada por um locador, que decide quais
  dados cadastrar e para quê. O locador responde pelas finalidades do tratamento.
- **Operador:** o **provedor de IA** configurado na instância, usado apenas na
  leitura assistida de documentos (extrair campos de um contrato antigo, de um
  RG/CNH ou de um comprovante) e na **comparação de fotos de vistoria**. O modelo
  usado é o configurado em `AI_MODEL` (ex.: `deepseek-v4.1-flash`). O provedor
  trata os dados somente para devolver a leitura, seguindo as instruções do
  locador — e o resultado que ele devolve é conferido pelo código, não aplicado
  direto (a comparação de vistorias sai como subsídio, sem baixa nem registro de
  dano automático).
- **Aluxen (software):** o programa em si. Não é controlador nem operador: é o
  instrumento que o locador usa. O Aluxen não recebe cópia dos dados do locador —
  cada instância guarda o seu próprio banco (SQLite ou Postgres) e o seu próprio
  armazenamento de arquivos.

---

## 2. Quais dados o sistema trata

**Do locatário (inquilino):**

- Nome, CPF, e-mail, telefone/WhatsApp.
- Endereço, quando aparece no contrato, no comprovante de residência ou no
  cadastro do imóvel vinculado.
- Senha de acesso à Área do Cliente (guardada só como hash, nunca em texto puro).

**Documentos enviados pelo locatário ou pelo locador:**

- Comprovantes de pagamento (para dar baixa em recibo, com conferência humana).
- Documento de identidade — RG ou CNH (no cadastro assistido por documento).
- Comprovante de residência (no cadastro assistido por documento).
- Contrato de locação antigo (para propor um cadastro novo).
- Fotos de vistoria de entrada e saída do imóvel.

**Do locador (dono da instância):**

- Nome, CPF, e-mail e endereço, usados nos cabeçalhos e nas assinaturas dos
  documentos emitidos.

**Da operação:**

- Trilha de auditoria: quem fez o quê, quando, de qual IP e o que mudou. Serve
  para investigar incidentes e comprovar o histórico. Senhas, tokens e cookies
  nunca entram na trilha.

---

## 3. Para que os dados são usados

- Emitir recibos e contratos e mantê-los vinculados ao locatário e ao imóvel.
- Cobrar aluguel e dar baixa nos pagamentos (sempre com conferência humana).
- Enviar mensagens e documentos pelo WhatsApp, quando o canal está configurado.
- Ler documentos enviados e propor um cadastro — que só é gravado depois que uma
  pessoa confere.
- Manter a trilha de auditoria e permitir backup e restauração da base.

O sistema **não** usa os dados para publicidade, perfilamento comercial ou
qualquer finalidade diferente das acima.

---

## 4. Base legal

- **Execução de contrato:** os dados do locatário e do imóvel são tratados para
  cumprir o contrato de locação e emitir os documentos dele (art. 7º, V, da LGPD).
- **Cumprimento de obrigação legal ou regulatória:** guarda de registros fiscais e
  de auditoria (art. 7º, II).
- **Legítimo interesse:** segurança da instância, prevenção a fraude e trilha de
  auditoria (art. 7º, IX).
- **Consentimento:** quando o locador ou o locatário envia um documento para
  leitura assistida, esse envio é a manifestação de vontade para aquela leitura
  (art. 7º, I). O documento pode ser descartado a qualquer momento na tela de
  conferência, sem virar cadastro.

---

## 5. Quem recebe os dados (compartilhamento)

- **Provedor de IA (`AI_MODEL`):** recebe o arquivo enviado para leitura somente
  quando o locador usa o cadastro assistido por documento. Fora desse fluxo, nada
  é enviado a ele.
- **Servidor de WhatsApp (WAHA ou Cloud API):** recebe o telefone e o texto da
  mensagem quando o locador envia um recibo, contrato ou cobrança pelo canal.
- **Provedor de hospedagem:** guarda a instância (container e volume). O locador
  escolhe esse provedor.

Os dados **não** são vendidos, alugados ou cedidos para terceiros com finalidade
própria deles. **Não há treinamento de modelo de IA com dados do cliente** — o
provedor de IA é usado como operador, apenas para devolver a leitura do documento
enviado.

---

## 6. Por quanto tempo os dados ficam guardados

- **Cadastro e documentos emitidos (contratos, recibos):** enquanto durar a
  relação de locação e pelos prazos legais aplicáveis (fiscais e civis).
- **Documentos enviados para leitura assistida:** até a conferência — confirmados
  viram cadastro; descartados são removidos com o motivo registrado.
- **Trilha de auditoria:** mantida para investigação de incidentes e comprovação
  do histórico.
- **Backups:** ficam no volume da instância enquanto o locador os mantiver. Apagar
  o volume da instância remove os dados daquela instância.

Como cada instância é isolada, o prazo é definido pelo locador dono dela. O
locatário pede a exclusão ao locador (ver seção 7).

---

## 7. Direitos do titular e como exercê-los

O titular pode pedir, a qualquer momento:

- **Confirmação** de que o sistema trata os seus dados.
- **Acesso** aos dados e aos documentos.
- **Correção** de dado incompleto, inexato ou desatualizado.
- **Anonimização, bloqueio ou eliminação** de dado desnecessário ou excessivo.
- **Portabilidade** dos dados a outro fornecedor, quando aplicável.
- **Informação** sobre com quem os dados foram compartilhados.
- **Revogação do consentimento**, quando o tratamento se baseia nele.

**Como exercer:** o titular fala diretamente com o **locador** — o controlador da
instância onde os seus dados estão. O locador atende pelo canal que já usa com o
locatário (telefone, e-mail ou WhatsApp) e executa a correção ou a exclusão pelo
próprio Aluxen. Não existe um canal central do software: cada instância pertence
ao seu locador.

---

## 8. Segurança

- Cada instância tem banco e arquivos próprios, sem base compartilhada.
- Senhas são guardadas só como hash; sessão e CSRF são assinados com
  `SESSION_SECRET`.
- O acesso do locatário à Área do Cliente mostra apenas os documentos do próprio
  titular; nenhuma rota de escrita é exposta a ele.
- A IA lê o documento, mas **não** dá baixa nem cria cadastro: a decisão é sempre
  de código determinístico e de conferência humana.

---

## 9. O que o Aluxen não faz

- **Não vende** dados pessoais.
- **Não treina** modelo de IA com dado do cliente.
- **Não** usa os dados para publicidade ou perfilamento.
- **Não** dá baixa em pagamento por saída de LLM: a IA só lê o documento; quem
  decide é a regra determinística, com revisão humana na dúvida.
