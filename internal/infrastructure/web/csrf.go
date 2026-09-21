package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
)

// CSRF por double-submit assinado: o token nasce de crypto/rand, é assinado com
// HMAC-SHA256 usando SESSION_SECRET, vai no cookie (não legível por JS) e é
// renderizado no campo oculto do formulário. Toda rota mutante exige que o valor
// enviado seja igual ao do cookie E que a assinatura confira, comparando com
// hmac.Equal (tempo constante).
//
// Ficam de fora apenas webhooks com autenticação própria (assinatura do WAHA).

const (
	csrfCookieName = "csrf_token"
	csrfFieldName  = "csrf_token"
)

// CSRF emite e valida os tokens de formulário.
type CSRF struct {
	secret []byte
	secure bool
}

// NewCSRF cria o validador de CSRF.
func NewCSRF(secret string, secure bool) *CSRF {
	return &CSRF{secret: []byte(secret), secure: secure}
}

// FieldName é o nome do campo oculto esperado nos formulários.
func (c *CSRF) FieldName() string { return csrfFieldName }

// Token reaproveita o cookie válido ou emite um novo, devolvendo o valor que o
// template renderiza no campo oculto.
func (c *CSRF) Token(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(csrfCookieName); err == nil && c.valid(cookie.Value) {
		return cookie.Value
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return ""
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	token := encoded + "." + c.mac(encoded)
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
	return token
}

// Middleware rejeita requisições mutantes sem token válido.
func (c *CSRF) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !mutatingMethod(r.Method) || csrfExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(csrfCookieName)
		if err != nil {
			http.Error(w, "csrf: token ausente", http.StatusForbidden)
			return
		}
		sent := r.FormValue(csrfFieldName)
		if sent == "" || !hmac.Equal([]byte(sent), []byte(cookie.Value)) || !c.valid(sent) {
			http.Error(w, "csrf: token inválido", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (c *CSRF) valid(token string) bool {
	encoded, signature, found := strings.Cut(token, ".")
	if !found {
		return false
	}
	return hmac.Equal([]byte(signature), []byte(c.mac(encoded)))
}

func (c *CSRF) mac(encoded string) string {
	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func mutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func csrfExempt(path string) bool {
	return strings.HasPrefix(path, "/api/whatsapp/webhook")
}
