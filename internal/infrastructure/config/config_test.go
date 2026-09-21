package config

import (
	"errors"
	"strings"
	"testing"
)

// configEnvVars são todas as variáveis lidas por Config. O pacote trata
// variável vazia como ausente (aplicando o padrão), então os testes as zeram
// para que o ambiente de quem roda a suíte não interfira.
var configEnvVars = []string{
	"APP_ENV", "PORT", "HOST", "DATABASE_URL", "DB_DIR", "LOG_LEVEL", "LOG_FORMAT",
	"SESSION_SECRET", "COOKIE_SECURE", "SEED_USERNAME", "SEED_PASSWORD", "SEED_DEMO",
	"DEFAULT_LANDLORD_NAME", "DEFAULT_LANDLORD_CPF", "APP_URL", "BASE_URL",
	"RATE_LIMIT_MAX", "RATE_LIMIT_GLOBAL_MAX", "LOGIN_LOCK_THRESHOLD",
	"WHATSAPP_MODE", "WAHA_API_URL", "WAHA_API_KEY", "WAHA_SESSION", "WAHA_WEBHOOK_SECRET",
	"AI_API_URL", "AI_API_KEY", "AI_MODEL", "AI_CONFIDENCE_THRESHOLD",
	"EASYPANEL_URL", "EASYPANEL_API_KEY",
}

// resetEnv zera todas as variáveis de configuração do teste.
func resetEnv(t *testing.T) {
	t.Helper()
	for _, name := range configEnvVars {
		t.Setenv(name, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	resetEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() devolveu erro inesperado: %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"AppEnv", cfg.AppEnv, "development"},
		{"Port", cfg.Port, 3000},
		{"Host", cfg.Host, "0.0.0.0"},
		{"DatabaseURL", cfg.DatabaseURL, ""},
		{"DBDir", cfg.DBDir, ""},
		{"LogLevel", cfg.LogLevel, "info"},
		{"LogFormat", cfg.LogFormat, ""},
		{"SessionSecret", cfg.SessionSecret, ""},
		{"CookieSecure", cfg.CookieSecure, false},
		{"SeedUsername", cfg.SeedUsername, "passoz"},
		{"SeedPassword", cfg.SeedPassword, "senha-de-teste"},
		{"SeedDemo", cfg.SeedDemo, false},
		{"DefaultLandlordName", cfg.DefaultLandlordName, "Locador"},
		{"DefaultLandlordCPF", cfg.DefaultLandlordCPF, ""},
		{"AppURL", cfg.AppURL, ""},
		{"BaseURL", cfg.BaseURL, ""},
		{"RateLimitMax", cfg.RateLimitMax, 5},
		{"RateLimitGlobalMax", cfg.RateLimitGlobalMax, 60},
		{"LoginLockThreshold", cfg.LoginLockThreshold, 10},
		{"WhatsAppMode", cfg.WhatsAppMode, "demo"},
		{"WahaAPIURL", cfg.WahaAPIURL, ""},
		{"WahaAPIKey", cfg.WahaAPIKey, ""},
		{"WahaSession", cfg.WahaSession, "default"},
		{"WahaWebhookSecret", cfg.WahaWebhookSecret, ""},
		{"AIAPIURL", cfg.AIAPIURL, ""},
		{"AIAPIKey", cfg.AIAPIKey, ""},
		{"AIModel", cfg.AIModel, ""},
		{"AIConfidenceThreshold", cfg.AIConfidenceThreshold, 0.9},
		{"EasypanelURL", cfg.EasypanelURL, ""},
		{"EasypanelAPIKey", cfg.EasypanelAPIKey, ""},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %#v, esperado %#v", check.name, check.got, check.want)
		}
	}

	if cfg.IsProduction() {
		t.Error("AppEnv padrão deveria ser de desenvolvimento")
	}
}

func TestLoadOverridesFromEnvironment(t *testing.T) {
	resetEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("PORT", "8080")
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("DATABASE_URL", "postgres://aluxen:aluxen@localhost:5432/aluxen")
	t.Setenv("DB_DIR", "/data")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("SESSION_SECRET", "segredo-de-producao-com-tamanho")
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("SEED_USERNAME", "locador")
	t.Setenv("SEED_PASSWORD", "outra-senha")
	t.Setenv("SEED_DEMO", "true")
	t.Setenv("DEFAULT_LANDLORD_NAME", "Maria")
	t.Setenv("DEFAULT_LANDLORD_CPF", "12345678900")
	t.Setenv("APP_URL", "https://aluxen.example.com")
	t.Setenv("BASE_URL", "https://base.example.com")
	t.Setenv("RATE_LIMIT_MAX", "9")
	t.Setenv("RATE_LIMIT_GLOBAL_MAX", "120")
	t.Setenv("LOGIN_LOCK_THRESHOLD", "3")
	t.Setenv("WHATSAPP_MODE", "client")
	t.Setenv("WAHA_API_URL", "http://waha:3000")
	t.Setenv("WAHA_API_KEY", "waha-key")
	t.Setenv("WAHA_SESSION", "aluxen")
	t.Setenv("WAHA_WEBHOOK_SECRET", "webhook-secret")
	t.Setenv("AI_API_URL", "http://ai:11434")
	t.Setenv("AI_API_KEY", "ai-key")
	t.Setenv("AI_MODEL", "qwen2.5-vl")
	t.Setenv("AI_CONFIDENCE_THRESHOLD", "0.75")
	t.Setenv("EASYPANEL_URL", "https://easypanel.example.com")
	t.Setenv("EASYPANEL_API_KEY", "easypanel-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() devolveu erro inesperado: %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"AppEnv", cfg.AppEnv, "production"},
		{"Port", cfg.Port, 8080},
		{"Host", cfg.Host, "127.0.0.1"},
		{"DatabaseURL", cfg.DatabaseURL, "postgres://aluxen:aluxen@localhost:5432/aluxen"},
		{"DBDir", cfg.DBDir, "/data"},
		{"LogLevel", cfg.LogLevel, "debug"},
		{"LogFormat", cfg.LogFormat, "json"},
		{"SessionSecret", cfg.SessionSecret, "segredo-de-producao-com-tamanho"},
		{"CookieSecure", cfg.CookieSecure, true},
		{"SeedUsername", cfg.SeedUsername, "locador"},
		{"SeedPassword", cfg.SeedPassword, "outra-senha"},
		{"SeedDemo", cfg.SeedDemo, true},
		{"DefaultLandlordName", cfg.DefaultLandlordName, "Maria"},
		{"DefaultLandlordCPF", cfg.DefaultLandlordCPF, "12345678900"},
		{"AppURL", cfg.AppURL, "https://aluxen.example.com"},
		{"BaseURL", cfg.BaseURL, "https://base.example.com"},
		{"RateLimitMax", cfg.RateLimitMax, 9},
		{"RateLimitGlobalMax", cfg.RateLimitGlobalMax, 120},
		{"LoginLockThreshold", cfg.LoginLockThreshold, 3},
		{"WhatsAppMode", cfg.WhatsAppMode, "client"},
		{"WahaAPIURL", cfg.WahaAPIURL, "http://waha:3000"},
		{"WahaAPIKey", cfg.WahaAPIKey, "waha-key"},
		{"WahaSession", cfg.WahaSession, "aluxen"},
		{"WahaWebhookSecret", cfg.WahaWebhookSecret, "webhook-secret"},
		{"AIAPIURL", cfg.AIAPIURL, "http://ai:11434"},
		{"AIAPIKey", cfg.AIAPIKey, "ai-key"},
		{"AIModel", cfg.AIModel, "qwen2.5-vl"},
		{"AIConfidenceThreshold", cfg.AIConfidenceThreshold, 0.75},
		{"EasypanelURL", cfg.EasypanelURL, "https://easypanel.example.com"},
		{"EasypanelAPIKey", cfg.EasypanelAPIKey, "easypanel-key"},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %#v, esperado %#v", check.name, check.got, check.want)
		}
	}

	if !cfg.IsProduction() {
		t.Error("APP_ENV=production deveria marcar a instância como produção")
	}
}

func TestLoadRejectsWeakSessionSecretInProduction(t *testing.T) {
	cases := []struct {
		name   string
		secret string
		wantOK bool
	}{
		{"ausente", "", false},
		{"curto demais", strings.Repeat("a", minSessionSecret-1), false},
		{"no limite", strings.Repeat("a", minSessionSecret), true},
		{"longo", strings.Repeat("a", 64), true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			resetEnv(t)
			t.Setenv("APP_ENV", "production")
			t.Setenv("SESSION_SECRET", testCase.secret)

			cfg, err := Load()
			if testCase.wantOK {
				if err != nil {
					t.Fatalf("Load() devolveu erro inesperado: %v", err)
				}
				if cfg.SessionSecret != testCase.secret {
					t.Errorf("SessionSecret = %q, esperado %q", cfg.SessionSecret, testCase.secret)
				}
				return
			}
			if err == nil {
				t.Fatal("Load() deveria recusar o boot em produção sem segredo próprio")
			}
			if !errors.Is(err, ErrSessionSecret) {
				t.Errorf("erro = %v, esperado embrulhar ErrSessionSecret", err)
			}
		})
	}
}

func TestLoadAllowsMissingSessionSecretOutsideProduction(t *testing.T) {
	resetEnv(t)
	t.Setenv("APP_ENV", "development")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() devolveu erro inesperado: %v", err)
	}
	if cfg.SessionSecret != "" {
		t.Errorf("SessionSecret = %q, esperado vazio fora de produção", cfg.SessionSecret)
	}
}

func TestLoadRejectsInvalidNumber(t *testing.T) {
	resetEnv(t)
	t.Setenv("PORT", "oitenta")

	if _, err := Load(); err == nil {
		t.Fatal("Load() deveria recusar PORT não numérica")
	}
}

func TestIsProduction(t *testing.T) {
	cases := map[string]bool{
		"production":   true,
		"PRODUCTION":   true,
		" production ": true,
		"development":  false,
		"staging":      false,
		"":             false,
	}
	for appEnv, want := range cases {
		cfg := &Config{AppEnv: appEnv}
		if got := cfg.IsProduction(); got != want {
			t.Errorf("IsProduction() com APP_ENV=%q = %v, esperado %v", appEnv, got, want)
		}
	}
}

func TestSecureCookies(t *testing.T) {
	cases := []struct {
		name       string
		appEnv     string
		cookieFlag bool
		want       bool
	}{
		{"desenvolvimento sem COOKIE_SECURE", "development", false, false},
		{"desenvolvimento com COOKIE_SECURE", "development", true, true},
		{"produção força Secure", "production", false, true},
		{"produção com COOKIE_SECURE", "production", true, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := &Config{AppEnv: testCase.appEnv, CookieSecure: testCase.cookieFlag}
			if got := cfg.SecureCookies(); got != testCase.want {
				t.Errorf("SecureCookies() = %v, esperado %v", got, testCase.want)
			}
		})
	}
}
