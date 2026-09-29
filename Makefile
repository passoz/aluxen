# ==============================================================================
# Aluxen — Ambientes de Execução (dev / test / prod)
# ==============================================================================
# - make dev:  Live-reload local com Air (sem container)
# - make test: Ambiente de testes com PostgreSQL provisionado exclusivamente via Podman
# - make prod: Ambiente de produção via Compose (multi-stage build), suportando Docker e Podman
# ==============================================================================

SHELL := /bin/bash
.DEFAULT_GOAL := help

# ── Resolução de Ferramentas e Engine de Produção ───────────────────────────
AIR_BIN := $(shell which air 2>/dev/null || (which go >/dev/null 2>&1 && echo "$$(go env GOPATH)/bin/air"))
PODMAN_BIN := $(shell which podman 2>/dev/null)
DOCKER_BIN := $(shell which docker 2>/dev/null)

# Engine de produção: 'docker' ou 'podman' (padrão: auto-detecção)
ENGINE ?= auto

ifeq ($(ENGINE),auto)
  ifneq ($(DOCKER_BIN),)
    PROD_ENGINE := docker
  else ifneq ($(PODMAN_BIN),)
    PROD_ENGINE := podman
  else
    PROD_ENGINE := docker
  endif
else ifeq ($(ENGINE),podman)
  PROD_ENGINE := podman
else ifeq ($(ENGINE),docker)
  PROD_ENGINE := docker
else
  $(error ENGINE inválido '$(ENGINE)'. Escolha 'docker' ou 'podman')
endif

# Comando compose para produção de acordo com a engine escolhida
ifeq ($(PROD_ENGINE),podman)
  PODMAN_COMPOSE_CHECK := $(shell podman compose version >/dev/null 2>&1 && echo "podman compose" || (which podman-compose 2>/dev/null || echo "podman compose"))
  PROD_COMPOSE := $(PODMAN_COMPOSE_CHECK)
else
  PROD_COMPOSE := docker compose
endif

# ── Help ─────────────────────────────────────────────────────────────────────
.PHONY: help
help: ## Exibe esta mensagem de ajuda com os alvos disponíveis
	@echo "=================================================================="
	@echo "  Aluxen — Ambientes de Execução"
	@echo "=================================================================="
	@echo "  make dev         Sobe ambiente de desenvolvimento com live-reload via Air"
	@echo "  make test        Sobe dependências com Podman e roda suíte completa com cobertura"
	@echo "  make prod        Gera e sobe ambiente de prod via Compose (Docker ou Podman)"
	@echo "  make prod-down   Para o ambiente de produção Compose"
	@echo "  make prod-logs   Acompanha logs do ambiente de produção"
	@echo ""
	@echo "Opções de produção:"
	@echo "  make prod ENGINE=docker   (usa Docker & Docker Compose)"
	@echo "  make prod ENGINE=podman   (usa Podman & Podman Compose)"
	@echo "  Engine detectada atual:   $(PROD_ENGINE)"
	@echo "=================================================================="

# ── DEV: Air com Live-Reload (sem container) ─────────────────────────────────
.PHONY: dev
dev: ## Executa o app em modo desenvolvimento com live-reload via Air
	@echo "==> [dev] Iniciando ambiente de desenvolvimento com Air..."
	@if [ -z "$(AIR_BIN)" ] || [ ! -x "$(AIR_BIN)" ]; then \
		echo "==> Air não encontrado no PATH. Instalando via 'go install github.com/air-verse/air@latest'..."; \
		go install github.com/air-verse/air@latest || { echo "ERRO: Falha ao instalar Air"; exit 1; }; \
	fi
	@mkdir -p data/storage
	@"$(if $(wildcard $(AIR_BIN)),$(AIR_BIN),air)" -c .air.toml

# ── TEST: Suíte completa com PostgreSQL provisionado via Podman ──────────────
.PHONY: test
test: ## Sobe PostgreSQL com Podman, executa testes com -race e checa cobertura >= 90%
	@echo "==> [test] Preparando ambiente de testes (exclusivamente via Podman)..."
	@if [ -z "$(PODMAN_BIN)" ]; then \
		echo "ERRO: Podman não encontrado no sistema. O ambiente de teste usa exclusivamente Podman."; \
		exit 1; \
	fi
	@if [ "$(ENGINE)" = "docker" ]; then \
		echo "ERRO: O ambiente de teste não permite Docker. Somente Podman é suportado para testes."; \
		exit 1; \
	fi
	@echo "==> [test] Subindo container PostgreSQL 17 com Podman..."
	@$(PODMAN_BIN) rm -f aluxen-test-postgres >/dev/null 2>&1 || true
	@$(PODMAN_BIN) run -d --name aluxen-test-postgres -p 5432:5432 \
		-e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=aluxen \
		docker.io/library/postgres:17 || { \
			echo "ERRO: Falha ao iniciar container PostgreSQL via Podman."; \
			echo "Nome completo da imagem (docker.io/library/postgres:17) evita o erro de short-name."; \
			echo "Se a mensagem acima for de conexão, verifique o socket do Podman (ex: systemctl --user start podman.socket)."; \
			exit 1; \
		}
	@bash -c '\
		trap "$(PODMAN_BIN) stop aluxen-test-postgres >/dev/null 2>&1; $(PODMAN_BIN) rm -f aluxen-test-postgres >/dev/null 2>&1 || true" EXIT INT TERM; \
		echo "==> [test] Aguardando PostgreSQL aceitar conexões..."; \
		for i in $$(seq 1 30); do \
			if $(PODMAN_BIN) exec aluxen-test-postgres pg_isready -U postgres -d aluxen >/dev/null 2>&1; then \
				echo "==> [test] PostgreSQL pronto!"; \
				break; \
			fi; \
			if [ $$i -eq 30 ]; then \
				echo "ERRO: Timeout aguardando inicialização do PostgreSQL no Podman."; \
				exit 1; \
			fi; \
			sleep 1; \
		done; \
		echo "==> [test] Executando suíte completa de testes (SQLite + PostgreSQL com -race)..."; \
		TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/aluxen?sslmode=disable" \
			go test ./... -race -coverpkg=./... -coverprofile=coverage.out; \
		echo "==> [test] Verificando gate de cobertura mínima (>= 90%)..."; \
		go tool cover -func=coverage.out | awk '\''/^total:/{print "==> Cobertura total: " $$3; if ($$3+0 < 90.0) { print "ERRO: Cobertura " $$3 " abaixo do limiar obrigatório de 90%"; exit 1 }}'\''; \
		echo "==> [test] Todos os testes passaram e cobertura >= 90% aprovada!"; \
	'

# ── PROD: Produção via Compose com Multi-Stage Build (Docker ou Podman) ─────
.PHONY: prod
prod: ## Sobe o ambiente de produção com Compose e multi-stage build (Docker ou Podman)
	@echo "==> [prod] Inicializando ambiente de produção..."
	@echo "==> [prod] Engine selecionada: $(PROD_ENGINE) (comando: $(PROD_COMPOSE))"
	@echo "==> [prod] Construindo imagem multi-stage e subindo stack..."
	@ALUXEN_BUILD_COMMIT="$$(git rev-parse HEAD 2>/dev/null || echo unknown)" \
	 ALUXEN_BUILD_COMMIT_SHORT="$$(git rev-parse --short HEAD 2>/dev/null || echo unknown)" \
	 ALUXEN_BUILD_TIME="$$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
	 ALUXEN_BUILD_NUMBER="$$(git rev-list --count HEAD 2>/dev/null || echo 1)" \
	 $(PROD_COMPOSE) -f docker-compose.yml up -d --build
	@echo "==> [prod] Stack em execução! Verificando status dos serviços..."
	@$(PROD_COMPOSE) -f docker-compose.yml ps
	@echo "==> [prod] Aluxen rodando em http://localhost:3000"

.PHONY: prod-down
prod-down: ## Encerra o ambiente de produção Compose
	@echo "==> [prod] Encerrando serviços de produção ($(PROD_ENGINE))..."
	@$(PROD_COMPOSE) -f docker-compose.yml down

.PHONY: prod-logs
prod-logs: ## Exibe e acompanha os logs do ambiente de produção
	@$(PROD_COMPOSE) -f docker-compose.yml logs -f

# ── Limpeza ──────────────────────────────────────────────────────────────────
.PHONY: clean
clean: ## Remove binários temporários, caches e arquivos de cobertura
	@rm -rf tmp coverage.out coverage.html
