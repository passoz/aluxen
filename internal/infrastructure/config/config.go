// Package config carrega a configuração da instância a partir do ambiente
// (12-factor). Nenhum valor sensível tem padrão embutido no código.
package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/caarlos0/env/v11"
)

// minSessionSecret é o tamanho mínimo aceito para SESSION_SECRET em produção.
const minSessionSecret = 16

// ErrSessionSecret indica configuração insegura de segredo de sessão.
//
// O app Bun anterior assinava tokens e cookies com um segredo padrão embutido no
// código-fonte, usado quando SESSION_SECRET não estava definido; qualquer
// instalação compartilhava o mesmo segredo, o que permitia forjar sessão. Aqui o
// boot em produção falha sem um segredo próprio e longo (fail-closed).
var ErrSessionSecret = errors.New("SESSION_SECRET deve ter ao menos 16 caracteres em produção")

// Config é a configuração resolvida da instância.
type Config struct {
	// AppEnv identifica o ambiente: `development`, `test` ou `production`.
	AppEnv string `env:"APP_ENV" envDefault:"development"`
	// Port é a porta TCP do servidor HTTP.
	Port int `env:"PORT" envDefault:"3000"`
	// Host é o endereço de escuta do servidor HTTP.
	Host string `env:"HOST" envDefault:"0.0.0.0"`
	// DatabaseURL é o DSN do banco; `file:...` resolve para SQLite e
	// `postgres://...` resolve para Postgres.
	DatabaseURL string `env:"DATABASE_URL"`
	// DBDir é o diretório do arquivo SQLite quando DATABASE_URL não é informado.
	DBDir string `env:"DB_DIR"`
	// LogLevel é o filtro do log: `debug`, `info`, `warn`, `error` ou `silent`.
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
	// LogFormat força o formato da saída (`json`); vazio segue o ambiente.
	LogFormat string `env:"LOG_FORMAT"`
	// SessionSecret assina os cookies de sessão e os tokens de autenticidade.
	// Em produção é obrigatório (ver ErrSessionSecret).
	SessionSecret string `env:"SESSION_SECRET"`
	// CookieSecure força o atributo `Secure` dos cookies fora de produção.
	CookieSecure bool `env:"COOKIE_SECURE" envDefault:"false"`
	// SeedUsername é o administrador criado no primeiro boot.
	SeedUsername string `env:"SEED_USERNAME" envDefault:"passoz"`
	// SeedPassword é a senha semeada do administrador no primeiro boot.
	SeedPassword string `env:"SEED_PASSWORD" envDefault:"senha-de-teste"`
	// SeedDemo pede que o boot popule a base com os dados de demonstração.
	SeedDemo bool `env:"SEED_DEMO" envDefault:"false"`
	// DefaultLandlordName é o nome do locador gravado no seed das configurações.
	DefaultLandlordName string `env:"DEFAULT_LANDLORD_NAME" envDefault:"Locador"`
	// DefaultLandlordCPF é o CPF do locador gravado no seed das configurações.
	DefaultLandlordCPF string `env:"DEFAULT_LANDLORD_CPF"`
	// AppURL é a URL pública da instância (vence BaseURL).
	AppURL string `env:"APP_URL"`
	// BaseURL é a URL pública da instância quando AppURL não é informado.
	BaseURL string `env:"BASE_URL"`
	// RateLimitMax é o limite de tentativas de login por IP na janela de 60s.
	RateLimitMax int `env:"RATE_LIMIT_MAX" envDefault:"5"`
	// RateLimitGlobalMax é o limite global de tentativas de login na janela de 60s.
	RateLimitGlobalMax int `env:"RATE_LIMIT_GLOBAL_MAX" envDefault:"60"`
	// LoginLockThreshold é o número de falhas que inicia o bloqueio progressivo.
	LoginLockThreshold int `env:"LOGIN_LOCK_THRESHOLD" envDefault:"10"`
	// WhatsAppMode é `demo` ou `client`.
	WhatsAppMode string `env:"WHATSAPP_MODE" envDefault:"demo"`
	// WahaAPIURL é a base da API do WAHA.
	WahaAPIURL string `env:"WAHA_API_URL"`
	// WahaAPIKey autentica as chamadas ao WAHA.
	WahaAPIKey string `env:"WAHA_API_KEY"`
	// WahaSession é a sessão do WAHA usada pela instância.
	WahaSession string `env:"WAHA_SESSION" envDefault:"default"`
	// WahaWebhookSecret valida a assinatura dos webhooks recebidos do WAHA.
	WahaWebhookSecret string `env:"WAHA_WEBHOOK_SECRET"`
	// AIAPIURL é a base da API do provedor de IA.
	AIAPIURL string `env:"AI_API_URL"`
	// AIAPIKey autentica as chamadas ao provedor de IA.
	AIAPIKey string `env:"AI_API_KEY"`
	// AIModel é o modelo usado nas extrações por IA.
	AIModel string `env:"AI_MODEL"`
	// AIConfidenceThreshold é a confiança mínima para a baixa automática.
	AIConfidenceThreshold float64 `env:"AI_CONFIDENCE_THRESHOLD" envDefault:"0.9"`
	// EasypanelURL é a base da API de provisionamento por instância.
	EasypanelURL string `env:"EASYPANEL_URL"`
	// EasypanelAPIKey autentica as chamadas à API de provisionamento.
	EasypanelAPIKey string `env:"EASYPANEL_API_KEY"`
}

// Load lê a configuração do ambiente e recusa o boot quando ela é insegura.
// Os erros embrulham a causa, de modo que o chamador usa erros.Is.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("configuração de ambiente inválida: %w", err)
	}
	if cfg.IsProduction() && len(cfg.SessionSecret) < minSessionSecret {
		return nil, fmt.Errorf("configuração de ambiente inválida: %w", ErrSessionSecret)
	}
	return cfg, nil
}

// IsProduction informa se a instância roda em produção.
func (c *Config) IsProduction() bool {
	return strings.EqualFold(strings.TrimSpace(c.AppEnv), "production")
}

// SecureCookies informa se os cookies de sessão levam o atributo `Secure`:
// COOKIE_SECURE=true ou produção.
func (c *Config) SecureCookies() bool {
	return c.CookieSecure || c.IsProduction()
}
