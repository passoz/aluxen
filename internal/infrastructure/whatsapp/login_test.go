package whatsapp

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── Dublê do cliente de sessão ─────────────────────────────────────

// fakeSessionClient simula o WAHA sem rede: devolve estados em sequência
// (repetindo o último) e conta as chamadas de ciclo de vida.
type fakeSessionClient struct {
	mu         sync.Mutex
	configured bool
	startErr   error
	stopErr    error
	logoutErr  error
	stateErr   error
	states     []LoginState
	started    int
	stopped    int
	loggedOut  int
}

func newFakeSessionClient(states ...LoginState) *fakeSessionClient {
	return &fakeSessionClient{configured: true, states: states}
}

func (f *fakeSessionClient) Configured() bool { return f.configured }

func (f *fakeSessionClient) StartLogin(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started++
	return f.startErr
}

func (f *fakeSessionClient) LoginState(context.Context) (LoginState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stateErr != nil {
		return LoginState{}, f.stateErr
	}
	if len(f.states) == 1 {
		return f.states[0], nil
	}
	if len(f.states) > 1 {
		state := f.states[0]
		f.states = f.states[1:]
		return state, nil
	}
	return LoginState{Status: LoginStateIdle}, nil
}

func (f *fakeSessionClient) CancelLogin(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped++
	return f.stopErr
}

func (f *fakeSessionClient) Logout(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loggedOut++
	return f.logoutErr
}

func (f *fakeSessionClient) counters() (started, stopped, loggedOut int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.started, f.stopped, f.loggedOut
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
}

type discardWriter struct{}

func (discardWriter) Write(payload []byte) (int, error) { return len(payload), nil }

func newTestManager(t *testing.T, client SessionClient, cfg LoginConfig) *LoginManager {
	t.Helper()
	return NewLoginManager(client, quietLogger(), cfg)
}

// waitForStatus espera o gerenciador chegar ao estado pedido.
func waitForStatus(t *testing.T, manager *LoginManager, status LoginStatus) LoginState {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if state := manager.State(); state.Status == status {
			return state
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("estado %q não chegou; estado atual %+v", status, manager.State())
	return LoginState{}
}

// ── Testes ────────────────────────────────────────────────────────

func TestLoginManagerStartsIdle(t *testing.T) {
	manager := newTestManager(t, newFakeSessionClient(), LoginConfig{})

	if state := manager.State(); state.Status != LoginStateIdle {
		t.Fatalf("estado inicial = %+v, esperado idle", state)
	}
	if !manager.Configured() {
		t.Fatal("gerenciador com cliente configurado não pode se dizer indisponível")
	}
}

func TestLoginManagerPublishesScanQRThenConnected(t *testing.T) {
	client := newFakeSessionClient(
		LoginState{Status: LoginStateScanQR, QRCode: "data:image/png;base64,QUJD", Message: QRCodeMessage},
		LoginState{Status: LoginStateConnected, Message: loginConnected},
	)
	manager := newTestManager(t, client, LoginConfig{PollInterval: 5 * time.Millisecond})

	started := manager.Start(context.Background())
	if started.Status != LoginStateStarting {
		t.Fatalf("estado após Start = %+v, esperado starting", started)
	}

	scanning := waitForStatus(t, manager, LoginStateScanQR)
	if scanning.QRCode != "data:image/png;base64,QUJD" || scanning.Message != QRCodeMessage {
		t.Fatalf("estado de leitura = %+v", scanning)
	}

	connected := waitForStatus(t, manager, LoginStateConnected)
	if connected.QRCode != "" {
		t.Fatalf("QR Code continuou visível depois de conectar: %+v", connected)
	}
	if connected.Message != loginConnected {
		t.Fatalf("mensagem = %q, esperado %q", connected.Message, loginConnected)
	}

	if started, stopped, _ := client.counters(); started != 1 || stopped != 0 {
		t.Fatalf("start=%d stop=%d, esperado start=1 stop=0", started, stopped)
	}
}

func TestLoginManagerKeepsSinglePairing(t *testing.T) {
	client := newFakeSessionClient(LoginState{Status: LoginStateScanQR, QRCode: "qr", Message: QRCodeMessage})
	manager := newTestManager(t, client, LoginConfig{PollInterval: 5 * time.Millisecond})

	manager.Start(context.Background())
	waitForStatus(t, manager, LoginStateScanQR)

	again := manager.Start(context.Background())
	if again.Status != LoginStateScanQR {
		t.Fatalf("segundo Start = %+v, esperado o pareamento em curso", again)
	}
	if started, _, _ := client.counters(); started != 1 {
		t.Fatalf("start=%d, esperado 1 (um pareamento por vez)", started)
	}
}

func TestLoginManagerCancelStopsPairing(t *testing.T) {
	client := newFakeSessionClient(LoginState{Status: LoginStateScanQR, QRCode: "qr", Message: QRCodeMessage})
	manager := newTestManager(t, client, LoginConfig{PollInterval: 5 * time.Millisecond})

	manager.Start(context.Background())
	waitForStatus(t, manager, LoginStateScanQR)

	if state := manager.Cancel(context.Background()); state.Status != LoginStateIdle {
		t.Fatalf("estado após cancelar = %+v, esperado idle", state)
	}
	if _, stopped, _ := client.counters(); stopped != 1 {
		t.Fatalf("stop=%d, esperado 1", stopped)
	}

	// O monitor cancelado não pode ressuscitar o pareamento.
	time.Sleep(30 * time.Millisecond)
	if state := manager.State(); state.Status != LoginStateIdle {
		t.Fatalf("estado depois do cancelamento = %+v, esperado idle", state)
	}
}

func TestLoginManagerCancelIgnoresClientFailure(t *testing.T) {
	client := newFakeSessionClient(LoginState{Status: LoginStateScanQR, QRCode: "qr"})
	client.stopErr = errors.New("WAHA fora do ar")
	manager := newTestManager(t, client, LoginConfig{PollInterval: 5 * time.Millisecond})

	manager.Start(context.Background())
	waitForStatus(t, manager, LoginStateScanQR)

	if state := manager.Cancel(context.Background()); state.Status != LoginStateIdle {
		t.Fatalf("estado após cancelar = %+v, esperado idle mesmo com erro no WAHA", state)
	}
}

func TestLoginManagerTimesOut(t *testing.T) {
	client := newFakeSessionClient(LoginState{Status: LoginStateScanQR, QRCode: "qr", Message: QRCodeMessage})
	manager := newTestManager(t, client, LoginConfig{Timeout: 40 * time.Millisecond, PollInterval: 5 * time.Millisecond})

	manager.Start(context.Background())

	state := waitForStatus(t, manager, LoginStateTimeout)
	if state.Error != loginTimeoutMessage {
		t.Fatalf("erro = %q, esperado %q", state.Error, loginTimeoutMessage)
	}
	if _, stopped, _ := client.counters(); stopped != 1 {
		t.Fatalf("stop=%d, esperado 1 (sessão vencida é parada)", stopped)
	}
}

func TestLoginManagerFailsWhenStartFails(t *testing.T) {
	client := newFakeSessionClient()
	client.startErr = errors.New("WAHA fora do ar")
	manager := newTestManager(t, client, LoginConfig{PollInterval: 5 * time.Millisecond})

	state := manager.Start(context.Background())
	if state.Status != LoginStateError || state.Error == "" {
		t.Fatalf("estado = %+v, esperado error com mensagem", state)
	}
}

func TestLoginManagerFailsAfterRepeatedLookupErrors(t *testing.T) {
	client := newFakeSessionClient()
	client.stateErr = errors.New("WAHA fora do ar")
	manager := newTestManager(t, client, LoginConfig{PollInterval: 5 * time.Millisecond})

	manager.Start(context.Background())

	state := waitForStatus(t, manager, LoginStateError)
	if state.Error != loginUnreachable {
		t.Fatalf("erro = %q, esperado %q", state.Error, loginUnreachable)
	}
}

func TestLoginManagerRefusesStartWithoutWAHA(t *testing.T) {
	client := newFakeSessionClient()
	client.configured = false
	manager := newTestManager(t, client, LoginConfig{})

	state := manager.Start(context.Background())
	if state.Status != LoginStateError || state.Error != loginUnavailable {
		t.Fatalf("estado = %+v, esperado error de instância sem canal", state)
	}
	if started, _, _ := client.counters(); started != 0 {
		t.Fatalf("start=%d, esperado 0 sem WAHA configurado", started)
	}
	if manager.Configured() {
		t.Fatal("Configured = true com cliente sem WAHA")
	}
}

func TestLoginManagerLogout(t *testing.T) {
	client := newFakeSessionClient(LoginState{Status: LoginStateConnected, Message: loginConnected})
	manager := newTestManager(t, client, LoginConfig{PollInterval: 5 * time.Millisecond})

	manager.Start(context.Background())
	waitForStatus(t, manager, LoginStateConnected)

	state, err := manager.Logout(context.Background())
	if err != nil {
		t.Fatalf("Logout devolveu erro: %v", err)
	}
	if state.Status != LoginStateIdle {
		t.Fatalf("estado após logout = %+v, esperado idle", state)
	}
	if _, _, loggedOut := client.counters(); loggedOut != 1 {
		t.Fatalf("logout=%d, esperado 1", loggedOut)
	}

	client.logoutErr = errors.New("WAHA fora do ar")
	if _, err := manager.Logout(context.Background()); err == nil {
		t.Fatal("esperava o erro do WAHA no logout")
	}
}

func TestLoginManagerLogoutWithoutWAHA(t *testing.T) {
	client := newFakeSessionClient()
	client.configured = false
	manager := newTestManager(t, client, LoginConfig{})

	if _, err := manager.Logout(context.Background()); err != nil {
		t.Fatalf("Logout sem WAHA devolveu erro: %v", err)
	}
	if _, _, loggedOut := client.counters(); loggedOut != 0 {
		t.Fatalf("logout=%d, esperado 0 chamadas ao WAHA", loggedOut)
	}
}

func TestLoginManagerDefaultsFollowTheCurrentApp(t *testing.T) {
	manager := newTestManager(t, newFakeSessionClient(), LoginConfig{})

	if manager.timeout != 3*time.Minute {
		t.Fatalf("timeout padrão = %v, esperado 3 minutos", manager.timeout)
	}
	if manager.pollInterval != 1500*time.Millisecond {
		t.Fatalf("intervalo padrão = %v, esperado 1,5 s", manager.pollInterval)
	}
}

func TestEncodePNGDataURL(t *testing.T) {
	encoded, err := encodePNGDataURL("2@abc")
	if err != nil {
		t.Fatalf("encodePNGDataURL devolveu erro: %v", err)
	}
	if _, ok := strings.CutPrefix(encoded, "data:image/png;base64,"); !ok {
		t.Fatalf("resultado não é data URL PNG: %q", encoded)
	}
}
