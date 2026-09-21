# Protocolo de Autenticidade e Validação Pública de Documentos

> **Documento legado (app Bun).** Descreve o protocolo como implementado no app
> Bun, **substituído no branch `devgo`** pelo port Go. Para a versão vigente —
> QR, URL pública e a dependência de hospedagem contínua — consulte
> [`VALIDACAO-GO.md`](VALIDACAO-GO.md). Mantido como referência histórica.

Este documento detalha o funcionamento técnico do sistema de **Validação Pública e Autenticidade Digital** implementado no Aluxen para **Recibos de Aluguel** e **Contratos de Locação**.

---

## 1. O Problema Resolvido

Tradicionalmente, recibos e contratos imobiliários em PDF podem ser facilmente adulterados por terceiros mal-intencionados (alteração de valores, datas de vencimento ou nomes de locatários).

Para garantir **fé pública**, **segurança jurídica** e **confiança**, o Aluxen implementa:
1. **Assinatura Criptográfica HMAC-SHA256** calculada sobre a entidade e seu identificador único.
2. **QR Code Dinâmico** incorporado nativamente ao PDF durante a geração.
3. **Código de Autenticação Alfanumérico Legível** (ex.: `AUTH-4061-AE09-3241-A94F`).
4. **Portal Público de Validação** acessível por qualquer pessoa sem necessidade de login.
5. **Acesso Público Seguro ao PDF Original** via token, permitindo que o locatário receba o link e baixe seu recibo sem precisar de conta de usuário no sistema.

---

## 2. Geração e Verificação de Tokens

O módulo `src/utils/auth-token.ts` gerencia as funções criptográficas:

### 2.1. Algoritmo de Geração (`generateAuthToken`)
```typescript
import { createHmac } from 'node:crypto';

const SECRET = process.env.SESSION_SECRET || 'aluxen-authenticity-secret-key-2026';

export function generateAuthToken(type: 'receipt' | 'lease', id: string): string {
  return createHmac('sha256', SECRET)
    .update(`${type}:${id}`)
    .digest('hex')
    .slice(0, 32);
}
```
- **Entrada**: Tipo do documento (`'receipt'` ou `'lease'`) e o UUID único da entidade.
- **Segredo**: `SESSION_SECRET` definido nas variáveis de ambiente.
- **Saída**: String hexadecimal de 32 caracteres (128 bits de entropia resistente a colisões).

### 2.2. Formatação do Código de Autenticidade (`formatAuthCode`)
Para facilitar a leitura e conferência manual em documentos físicos impressos:
```typescript
export function formatAuthCode(token: string): string {
  const clean = token.toUpperCase().replace(/[^A-Z0-9]/g, '');
  return `AUTH-${clean.slice(0, 4)}-${clean.slice(4, 8)}-${clean.slice(8, 12)}-${clean.slice(12, 16)}`;
}
// Exemplo: AUTH-6B95-7D1C-507C-46F9
```

### 2.3. Comparação Segura contra Timing Attacks (`verifyAuthToken`)
A verificação utiliza `crypto.timingSafeEqual` para evitar ataques baseados no tempo de resposta:
```typescript
import { timingSafeEqual } from 'node:crypto';

export function verifyAuthToken(type: 'receipt' | 'lease', id: string, token: string): boolean {
  if (!token || typeof token !== 'string') return false;
  const expected = generateAuthToken(type, id);
  if (token.length !== expected.length) return false;

  try {
    return timingSafeEqual(Buffer.from(token), Buffer.from(expected));
  } catch {
    return false;
  }
}
```

---

## 3. Resolução Inteligente da URL Pública (`resolvePublicBaseUrl`)

Como o Aluxen pode operar em desenvolvimento local, redes Wi-Fi domésticas, atrás de proxies reversos ou em servidores de produção na nuvem, o sistema determina a URL pública do QR Code com base na seguinte ordem de precedência:

1. **Configuração Salva no Banco de Dados**: Valor configurado pelo administrador na tela `/settings` (campo `app_url`).
2. **Variável de Ambiente Explícita**: `APP_URL` ou `BASE_URL` (ex.: `https://aluxen.evolucsia.com`).
3. **Padrão de Produção**: Em `NODE_ENV=production`, o padrão é `https://aluxen.evolucsia.com`.
4. **Proxy Reverso / Cabeçalhos HTTP**: Se vier através de requisição com cabeçalhos `x-forwarded-host` ou `host`, utiliza o domínio e protocolo da chamada real.
5. **Rede Local (Wi-Fi / LAN)**: Em ambiente de desenvolvimento local, detecta a interface de rede ativa da máquina (ex.: `192.168.10.109`) e monta `http://192.168.10.109:3000`. Isso permite que **qualquer smartphone conectado ao mesmo Wi-Fi aponte a câmera para a tela do computador ou para o PDF e abra imediatamente a página de validação** sem qualquer configuração extra.

---

## 4. O Portal Público de Validação

As páginas públicas de validação são servidas em:
- **Recibos**: `GET /validar/recibo/:id?token=...`
- **Contratos**: `GET /validar/contrato/:id?token=...`

### 4.1. Cenário: Documento Autêntico e Válido
Se o token for válido e o registro existir no banco de dados SQLite:
- É exibido o selo verde de **DOCUMENTO AUTÊNTICO E VÁLIDO**.
- São detalhados:
  - Tipo de documento (Recibo de Pagamento ou Contrato de Locação).
  - Chave de Autenticação (`AUTH-XXXX-...`).
  - Nome e CPF do locatário.
  - Identificação e endereço do imóvel.
  - Nome do locador / emitente.
  - Valor do documento e valor quitado (com suporte a pagamento parcial).
  - Data de vencimento e data de quitação.
  - Carimbo de data e hora em que a consulta foi realizada.
  - **Botão Oficial para Visualizar / Baixar o PDF Original** (`/pdf?id=...&token=...`).

### 4.2. Cenário: Documento Inválido ou Adulterado
Se o ID não existir, o token estiver ausente, incorreto ou expirado:
- O servidor retorna status HTTP 400.
- É exibido um card vermelho de alerta informando que o documento **não pôde ter sua autenticidade comprovada**, alertando o usuário sobre possíveis adulterações ou códigos inválidos.

---

## 5. Acesso Público ao PDF com Token

Para compartilhar comprovantes diretamente com o locatário sem exigir que ele tenha usuário ou senha no Aluxen:
- A rota `/pdf?id=...&token=...` e `/contract-pdf?id=...&token=...` aceitam o parâmetro `token`.
- Se o usuário estiver deslogado, mas fornecer um `token` válido que confira com o `id` solicitado, o documento PDF é liberado para visualização e download diretamente pelo navegador.
- Se o token não for enviado ou for inválido e não houver cookie de sessão ativo, a requisição é redirecionada para `/login`.

---

## 6. Boas Práticas de Segurança

1. **Defina um `SESSION_SECRET` Forte em Produção**:
   - Utilize uma chave longa e aleatória de pelo menos 32 caracteres (ex.: `openssl rand -hex 32`).
   - Se o segredo for alterado, tokens emitidos anteriormente serão invalidados.
2. **Use HTTPS em Produção**:
   - Garanta que todo o tráfego do portal de validação transite sobre TLS/HTTPS para prevenir ataques de interceptação man-in-the-middle.
