# Aluxen

Sistema de gestão de locações.

## Docker

```bash
# Rodar em produção
docker compose up --build

# Parar
docker compose down
```

### Variáveis de Ambiente

| Variável | Padrão | Descrição |
|---------|--------|-----------|
| `DB_DIR` | `/data` | Diretório do banco de dados |
| `SESSION_SECRET` | `production-secret-change-me` | Chave para sessões |
| `SEED_USERNAME` | `passoz` | Usuário admin inicial |
| `SEED_PASSWORD` | `Dt32btop@` | Senha admin inicial |

### Volume

O banco SQLite é persistido no volume `aluxen-data`. Os dados ficam em `/data/aluxen.db` dentro do container.

## Desenvolvimento Local

```bash
npm install
npm run dev
```

## Testes

```bash
npm run test        # unitários
npm run test:e2e   # end-to-end
```

## GitHub Actions

O workflow `.github/workflows/publish-image.yml` gera e publica a imagem em `registry.evolucsia.com/passoz/aluxen` em pushes para `main` e em execução manual.

Secrets necessários no repositório:

- `REGISTRY_USERNAME`
- `REGISTRY_PASSWORD`

Tags publicadas:

- `latest`
- `sha-<commit>`
