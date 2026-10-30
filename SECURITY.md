# Security Policy

## Reportar uma vulnerabilidade

Não abra uma issue pública. Envie um e-mail para **fernandopassoz@gmail.com** com:

- a versão ou o commit afetado;
- o passo a passo para reproduzir;
- o impacto esperado;
- uma sugestão de correção, se tiver.

Você recebe uma resposta em até 7 dias. Corrigi a falha antes de divulgar os detalhes.

## Escopo

O Aluxen é uma instância por locador, sem multiusuário e sem multitenancy. Implantação exposta à
internet deve usar HTTPS e um proxy com autenticação adicional, se necessário.

Falhas de interesse:

- execução remota de código, injeção e path traversal;
- vazamento de `SESSION_SECRET`, chave de autenticidade ou credencial de provedor;
- bypass de CSRF, de rate limit ou da troca obrigatória de senha;
- acesso de um locatário a contrato, recibo ou documento de outro titular;
- validação pública que revele dados além do documento consultado.

## Boas práticas

- `SESSION_SECRET` e `SEED_PASSWORD` vivem apenas no ambiente; nunca no repositório.
- `SEED_PASSWORD` é obrigatória no primeiro boot e a troca é exigida no primeiro login.
- O CI roda Gitleaks e `govulncheck` em todo push.
