package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Sessão por cookie assinado.
//
// Diferença deliberada em relação ao app Bun: o cookie antigo guardava o id cru,
// o que permitia forjar sessão de qualquer usuário conhecendo o id. Aqui o valor
// é `id|expiração` assinado com HMAC-SHA256 usando SESSION_SECRET, comparado em
// tempo constante. Nomes, flags e validade continuam iguais.

const (
	adminCookieName  = "__aluxen_session"
	clientCookieName = "__aluxen_client_session"
	sessionTTL       = 30 * 24 * time.Hour
)

// SessionManager emite e valida os cookies de sessão.
type SessionManager struct {
	secret []byte
	secure bool
}

// NewSessionManager cria o gerenciador de sessão.
func NewSessionManager(secret string, secure bool) *SessionManager {
	return &SessionManager{secret: []byte(secret), secure: secure}
}

// SetAdmin grava a sessão do administrador.
func (m *SessionManager) SetAdmin(w http.ResponseWriter, userID string) {
	m.set(w, adminCookieName, userID)
}

// AdminID devolve o id do administrador autenticado, ou string vazia.
func (m *SessionManager) AdminID(r *http.Request) string {
	return m.get(r, adminCookieName)
}

// ClearAdmin remove a sessão do administrador.
func (m *SessionManager) ClearAdmin(w http.ResponseWriter) {
	m.clear(w, adminCookieName)
}

// SetClient grava a sessão do locatário.
func (m *SessionManager) SetClient(w http.ResponseWriter, tenantID string) {
	m.set(w, clientCookieName, tenantID)
}

// ClientID devolve o id do locatário autenticado, ou string vazia.
func (m *SessionManager) ClientID(r *http.Request) string {
	return m.get(r, clientCookieName)
}

// ClearClient remove a sessão do locatário.
func (m *SessionManager) ClearClient(w http.ResponseWriter) {
	m.clear(w, clientCookieName)
}

func (m *SessionManager) set(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    m.sign(value),
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL / time.Second),
	})
}

func (m *SessionManager) get(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return m.verify(cookie.Value)
}

func (m *SessionManager) clear(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (m *SessionManager) sign(value string) string {
	payload := value + "|" + strconv.FormatInt(time.Now().Add(sessionTTL).Unix(), 10)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encoded + "." + m.mac(encoded)
}

func (m *SessionManager) verify(token string) string {
	encoded, signature, found := strings.Cut(token, ".")
	if !found || !hmac.Equal([]byte(signature), []byte(m.mac(encoded))) {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	value, expiresAt, found := strings.Cut(string(payload), "|")
	if !found {
		return ""
	}
	deadline, err := strconv.ParseInt(expiresAt, 10, 64)
	if err != nil || time.Now().Unix() > deadline {
		return ""
	}
	return value
}

func (m *SessionManager) mac(encoded string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
