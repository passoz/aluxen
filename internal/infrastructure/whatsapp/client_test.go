package whatsapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ── Servidor falso do WAHA ─────────────────────────────────────────

// recordedRequest é uma chamada recebida pelo WAHA falso.
type recordedRequest struct {
	method string
	path   string
	query  string
	header http.Header
	body   string
}

// fakeWaha serve a API do WAHA em memória e guarda o que recebeu. Nenhum teste
// deste pacote toca a rede real.
type fakeWaha struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	requests []recordedRequest
}

func newFakeWaha(t *testing.T, handler http.HandlerFunc) *fakeWaha {
	t.Helper()
	fake := &fakeWaha{t: t}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fake.mu.Lock()
		fake.requests = append(fake.requests, recordedRequest{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			header: r.Header.Clone(),
			body:   string(body),
		})
		fake.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeWaha) last() recordedRequest {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		f.t.Fatal("nenhuma chamada chegou ao WAHA falso")
	}
	return f.requests[len(f.requests)-1]
}

func (f *fakeWaha) client(config ClientConfig) *Client {
	config.BaseURL = f.server.URL
	return NewClient(config)
}

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *fakeWaha) {
	t.Helper()
	fake := newFakeWaha(t, handler)
	return fake.client(ClientConfig{APIKey: "segredo", Session: "aluxen"}), fake
}

func writeJSONResponse(w http.ResponseWriter, status int, payload string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(payload))
}

// ── Status ────────────────────────────────────────────────────────

func TestClientStatusReadsSessionState(t *testing.T) {
	client, fake := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"name":"aluxen","status":"WORKING"}`)
	})

	state, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status devolveu erro: %v", err)
	}
	if state != SessionStateWorking {
		t.Fatalf("estado = %q, esperado WORKING", state)
	}

	request := fake.last()
	if request.method != http.MethodGet || request.path != "/api/sessions/aluxen" {
		t.Fatalf("chamada inesperada: %s %s", request.method, request.path)
	}
	if key := request.header.Get("X-Api-Key"); key != "segredo" {
		t.Fatalf("X-Api-Key = %q, esperado segredo", key)
	}
}

func TestClientStatusMapsUnknownState(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"name":"aluxen","status":"WHATEVER"}`)
	})

	state, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status devolveu erro: %v", err)
	}
	if state != SessionStateUnknown {
		t.Fatalf("estado = %q, esperado UNKNOWN", state)
	}
}

func TestClientStatusFailsOnServerError(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusInternalServerError, `{"message":"boom"}`)
	})

	_, err := client.Status(context.Background())
	if err == nil {
		t.Fatal("esperava erro no status 500")
	}
	if !strings.Contains(err.Error(), "status 500") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("erro sem o status e o corpo do WAHA: %v", err)
	}
}

func TestClientStatusFailsOnInvalidBody(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"status":`)
	})

	if _, err := client.Status(context.Background()); err == nil {
		t.Fatal("esperava erro para corpo inválido")
	}
}

func TestClientWithoutBaseURLIsNotConfigured(t *testing.T) {
	client := NewClient(ClientConfig{})

	if client.Configured() {
		t.Fatal("cliente sem WAHA_API_URL não pode se dizer configurado")
	}
	if _, err := client.Status(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("erro = %v, esperado ErrNotConfigured", err)
	}
}

func TestClientTrimsBaseURL(t *testing.T) {
	fake := newFakeWaha(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"status":"STOPPED"}`)
	})
	client := NewClient(ClientConfig{BaseURL: fake.server.URL + "/", Session: "aluxen"})

	if _, err := client.Status(context.Background()); err != nil {
		t.Fatalf("Status devolveu erro: %v", err)
	}
	if path := fake.last().path; path != "/api/sessions/aluxen" {
		t.Fatalf("path = %q, esperado /api/sessions/aluxen", path)
	}
}

// ── Pareamento ────────────────────────────────────────────────────

func TestClientStartLoginPostsToStart(t *testing.T) {
	client, fake := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusCreated, `{"name":"aluxen","status":"STARTING"}`)
	})

	if err := client.StartLogin(context.Background()); err != nil {
		t.Fatalf("StartLogin devolveu erro: %v", err)
	}
	if request := fake.last(); request.method != http.MethodPost || request.path != "/api/sessions/aluxen/start" {
		t.Fatalf("chamada inesperada: %s %s", request.method, request.path)
	}
}

func TestClientLoginStateReturnsQRCodeImage(t *testing.T) {
	image := []byte{0x89, 'P', 'N', 'G', 0x00, 0x01}
	client, fake := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/sessions/aluxen":
			writeJSONResponse(w, http.StatusOK, `{"name":"aluxen","status":"SCAN_QR_CODE"}`)
		case "/api/aluxen/auth/qr":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(image)
		default:
			http.NotFound(w, r)
		}
	})

	state, err := client.LoginState(context.Background())
	if err != nil {
		t.Fatalf("LoginState devolveu erro: %v", err)
	}
	if state.Status != LoginStateScanQR {
		t.Fatalf("status = %q, esperado scan_qr", state.Status)
	}
	expected := "data:image/png;base64," + base64.StdEncoding.EncodeToString(image)
	if state.QRCode != expected {
		t.Fatalf("QRCode = %q, esperado %q", state.QRCode, expected)
	}
	if state.Message != QRCodeMessage {
		t.Fatalf("Message = %q, esperado o texto do app atual", state.Message)
	}

	request := fake.last()
	if request.query != "format=image" {
		t.Fatalf("query = %q, esperado format=image", request.query)
	}
	if accept := request.header.Get("Accept"); accept != "image/png" {
		t.Fatalf("Accept = %q, esperado image/png", accept)
	}
}

func TestClientLoginStateAcceptsDataURLFromJSON(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/sessions/aluxen":
			writeJSONResponse(w, http.StatusOK, `{"status":"SCAN_QR_CODE"}`)
		default:
			writeJSONResponse(w, http.StatusOK, `{"value":"data:image/png;base64,QUJD"}`)
		}
	})

	state, err := client.LoginState(context.Background())
	if err != nil {
		t.Fatalf("LoginState devolveu erro: %v", err)
	}
	if state.QRCode != "data:image/png;base64,QUJD" {
		t.Fatalf("QRCode = %q, esperado o data URL devolvido pelo WAHA", state.QRCode)
	}
}

func TestClientLoginStateEncodesRawQRCodeText(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/sessions/aluxen":
			writeJSONResponse(w, http.StatusOK, `{"status":"SCAN_QR_CODE"}`)
		default:
			writeJSONResponse(w, http.StatusOK, `{"value":"2@abc/def+gh="}`)
		}
	})

	state, err := client.LoginState(context.Background())
	if err != nil {
		t.Fatalf("LoginState devolveu erro: %v", err)
	}
	encoded, ok := strings.CutPrefix(state.QRCode, "data:image/png;base64,")
	if !ok {
		t.Fatalf("QRCode = %q, esperado um data URL PNG", state.QRCode)
	}
	png, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("QRCode não é base64: %v", err)
	}
	if len(png) < 8 || string(png[1:4]) != "PNG" {
		t.Fatalf("QRCode não é um PNG: %v", png[:8])
	}
}

func TestClientLoginStateConnectedClearsQRCode(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"status":"WORKING"}`)
	})

	state, err := client.LoginState(context.Background())
	if err != nil {
		t.Fatalf("LoginState devolveu erro: %v", err)
	}
	if state.Status != LoginStateConnected || state.QRCode != "" {
		t.Fatalf("estado = %+v, esperado connected sem QR Code", state)
	}
}

func TestClientLoginStateFailedAndStopped(t *testing.T) {
	cases := map[string]LoginStatus{
		"FAILED":  LoginStateError,
		"STOPPED": LoginStateIdle,
	}
	for raw, expected := range cases {
		client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSONResponse(w, http.StatusOK, `{"status":"`+raw+`"}`)
		})
		state, err := client.LoginState(context.Background())
		if err != nil {
			t.Fatalf("LoginState(%s) devolveu erro: %v", raw, err)
		}
		if state.Status != expected {
			t.Fatalf("status de %s = %q, esperado %q", raw, state.Status, expected)
		}
	}
}

func TestClientCancelLoginAndLogout(t *testing.T) {
	client, fake := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"status":"STOPPED"}`)
	})

	if err := client.CancelLogin(context.Background()); err != nil {
		t.Fatalf("CancelLogin devolveu erro: %v", err)
	}
	if request := fake.last(); request.method != http.MethodPost || request.path != "/api/sessions/aluxen/stop" {
		t.Fatalf("chamada inesperada: %s %s", request.method, request.path)
	}

	if err := client.Logout(context.Background()); err != nil {
		t.Fatalf("Logout devolveu erro: %v", err)
	}
	if request := fake.last(); request.method != http.MethodPost || request.path != "/api/sessions/aluxen/logout" {
		t.Fatalf("chamada inesperada: %s %s", request.method, request.path)
	}
}

func TestClientCancelLoginFailsOnServerError(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusInternalServerError, `{"message":"sem sessão"}`)
	})

	if err := client.CancelLogin(context.Background()); err == nil {
		t.Fatal("esperava erro no stop com status 500")
	}
	if err := client.Logout(context.Background()); err == nil {
		t.Fatal("esperava erro no logout com status 500")
	}
}

// ── Envio ─────────────────────────────────────────────────────────

func TestClientSendTextReturnsMessageID(t *testing.T) {
	client, fake := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusCreated, `{"id":{"fromMe":true,"_serialized":"false_5511999999999@c.us_ABC"}}`)
	})

	id, err := client.SendText(context.Background(), "5511999999999@c.us", "Olá!")
	if err != nil {
		t.Fatalf("SendText devolveu erro: %v", err)
	}
	if id != "false_5511999999999@c.us_ABC" {
		t.Fatalf("id = %q", id)
	}

	request := fake.last()
	if request.method != http.MethodPost || request.path != "/api/sendText" {
		t.Fatalf("chamada inesperada: %s %s", request.method, request.path)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(request.body), &payload); err != nil {
		t.Fatalf("corpo do envio não é JSON: %v", err)
	}
	if payload["session"] != "aluxen" || payload["chatId"] != "5511999999999@c.us" || payload["text"] != "Olá!" {
		t.Fatalf("corpo do envio inesperado: %v", payload)
	}
}

func TestClientSendTextAcceptsPlainStringID(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"id":"ABC"}`)
	})

	id, err := client.SendText(context.Background(), "5511999999999@c.us", "Oi")
	if err != nil {
		t.Fatalf("SendText devolveu erro: %v", err)
	}
	if id != "ABC" {
		t.Fatalf("id = %q, esperado ABC", id)
	}
}

func TestClientSendTextWithoutID(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusOK, `{"success":true}`)
	})

	id, err := client.SendText(context.Background(), "5511999999999@c.us", "Oi")
	if err != nil {
		t.Fatalf("SendText devolveu erro: %v", err)
	}
	if id != "" {
		t.Fatalf("id = %q, esperado vazio", id)
	}
}

func TestClientSendTextFailsOnServerErrorAndInvalidBody(t *testing.T) {
	serverError, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusInternalServerError, `{"message":"sessão não está WORKING"}`)
	})
	if _, err := serverError.SendText(context.Background(), "5511999999999@c.us", "Oi"); err == nil {
		t.Fatal("esperava erro no envio com status 500")
	}

	invalidBody, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusOK, `não é json`)
	})
	if _, err := invalidBody.SendText(context.Background(), "5511999999999@c.us", "Oi"); err == nil {
		t.Fatal("esperava erro no envio com corpo inválido")
	}
}

// ── Auxiliares ────────────────────────────────────────────────────

func TestMessageIDReadsNestedObject(t *testing.T) {
	cases := map[string]string{
		`"ABC"`:                      "ABC",
		`{"_serialized":"ABC"}`:      "ABC",
		`{"id":"ABC"}`:               "ABC",
		`{}`:                         "",
		`null`:                       "",
		`{"_serialized":""}`:         "",
		`{"_serialized":"","id":""}`: "",
	}
	for raw, expected := range cases {
		if got := messageID(json.RawMessage(raw)); got != expected {
			t.Fatalf("messageID(%s) = %q, esperado %q", raw, got, expected)
		}
	}
}
