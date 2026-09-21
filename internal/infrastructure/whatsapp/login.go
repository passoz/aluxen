package whatsapp

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// Estado do pareamento, no vocabulário do app atual: o painel já conhece estes
// valores e os textos das mensagens.
type LoginStatus string

const (
	LoginStateIdle      LoginStatus = "idle"
	LoginStateStarting  LoginStatus = "starting"
	LoginStateScanQR    LoginStatus = "scan_qr"
	LoginStateSyncing   LoginStatus = "syncing"
	LoginStateConnected LoginStatus = "connected"
	LoginStateError     LoginStatus = "error"
	LoginStateTimeout   LoginStatus = "timeout"
)

// LoginState é o estado do pareamento devolvido por /api/whatsapp/login/status.
type LoginState struct {
	Status  LoginStatus `json:"status"`
	QRCode  string      `json:"qrCode,omitempty"`
	Message string      `json:"message,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// Textos do pareamento. O do QR Code é literalmente o do app atual; os demais
// trocam o "navegador" pelo canal HTTP que os substituiu.
const (
	loginStartingMessage = "Iniciando sessão do WhatsApp..."
	loginTimeoutMessage  = "Tempo limite de 3 minutos esgotado. Tente novamente."
	loginUnavailable     = "Nenhum canal do WhatsApp configurado nesta instância. Defina WAHA_API_URL."
	loginUnreachable     = "Não foi possível consultar a sessão do WhatsApp."
	loginConnected       = "WhatsApp conectado com sucesso ao Aluxen!"
)

// Padrões do app atual: 3 minutos para concluir o pareamento, consulta a cada 1,5 s.
const (
	defaultLoginTimeout      = 3 * time.Minute
	defaultLoginPollInterval = 1500 * time.Millisecond
	// maxLoginFailures é quantas consultas seguidas podem falhar antes de o
	// pareamento ser dado como perdido (WAHA fora do ar, por exemplo).
	maxLoginFailures = 3
	// qrCodeSize é o lado da imagem do QR Code entregue ao painel.
	qrCodeSize = 280
)

// SessionClient é o subconjunto do cliente do WAHA usado pelo gerenciador de
// pareamento. Existe para que os testes injetem um dublê sem rede.
type SessionClient interface {
	StartLogin(ctx context.Context) error
	LoginState(ctx context.Context) (LoginState, error)
	CancelLogin(ctx context.Context) error
	Logout(ctx context.Context) error
}

// Availability informa se o canal está configurado. Implementado por *Client.
type Availability interface {
	Configured() bool
}

// LoginConfig ajusta os tempos do pareamento.
type LoginConfig struct {
	// Timeout é o limite para concluir o pareamento (padrão: 3 minutos).
	Timeout time.Duration
	// PollInterval é o intervalo entre consultas ao WAHA (padrão: 1,5 s).
	PollInterval time.Duration
}

// LoginManager conduz um único pareamento por vez: pede o start ao WAHA,
// acompanha a sessão em segundo plano até conectar (ou estourar o tempo) e
// publica o estado que o painel consulta.
type LoginManager struct {
	client       SessionClient
	log          *slog.Logger
	timeout      time.Duration
	pollInterval time.Duration
	configured   bool

	mu    sync.Mutex
	state LoginState
	// gen identifica o monitor em curso: um monitor antigo não escreve estado.
	gen  uint64
	stop chan struct{}
}

// NewLoginManager monta o gerenciador sobre o cliente do WAHA.
func NewLoginManager(client SessionClient, log *slog.Logger, cfg LoginConfig) *LoginManager {
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultLoginTimeout
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultLoginPollInterval
	}
	configured := true
	if probe, ok := client.(Availability); ok {
		configured = probe.Configured()
	}
	return &LoginManager{
		client:       client,
		log:          log,
		timeout:      cfg.Timeout,
		pollInterval: cfg.PollInterval,
		configured:   configured,
		state:        LoginState{Status: LoginStateIdle},
	}
}

// Configured informa se o canal está configurado nesta instância.
func (m *LoginManager) Configured() bool { return m.configured }

// State devolve o estado atual do pareamento.
func (m *LoginManager) State() LoginState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Start inicia o pareamento e devolve o estado inicial. Um pareamento em curso
// não é reiniciado: a chamada devolve o estado que já está valendo.
func (m *LoginManager) Start(ctx context.Context) LoginState {
	if !m.configured {
		return m.replace(LoginState{Status: LoginStateError, Error: loginUnavailable})
	}

	m.mu.Lock()
	if m.state.Status == LoginStateStarting || m.state.Status == LoginStateScanQR || m.state.Status == LoginStateSyncing {
		state := m.state
		m.mu.Unlock()
		return state
	}
	m.gen++
	m.detachMonitorLocked()
	m.state = LoginState{Status: LoginStateStarting, Message: loginStartingMessage}
	generation := m.gen
	m.mu.Unlock()

	if err := m.client.StartLogin(ctx); err != nil {
		m.log.Error("falha ao iniciar a sessão do WhatsApp no WAHA", "scope", "whatsapp", "err", err)
		if !m.apply(generation, LoginState{Status: LoginStateError, Error: "Não foi possível iniciar a sessão do WhatsApp no WAHA."}) {
			return m.State()
		}
		return m.State()
	}

	m.watch(generation)
	return m.State()
}

// Cancel interrompe o pareamento em curso e volta ao estado inicial. A sessão do
// WAHA é parada em melhor esforço: falhar nisso não muda o que o painel vê.
func (m *LoginManager) Cancel(ctx context.Context) LoginState {
	m.mu.Lock()
	m.gen++
	m.detachMonitorLocked()
	m.state = LoginState{Status: LoginStateIdle}
	m.mu.Unlock()

	if m.configured {
		if err := m.client.CancelLogin(ctx); err != nil {
			m.log.Warn("falha ao parar a sessão do WhatsApp no WAHA", "scope", "whatsapp", "err", err)
		}
	}
	return m.State()
}

// Logout encerra a sessão pareada e descarta o pareamento. Diferente do
// cancelamento, a falha do WAHA é devolvida: desconectar de verdade e não
// conseguir avisar o servidor não é o mesmo que desconectar.
func (m *LoginManager) Logout(ctx context.Context) (LoginState, error) {
	m.mu.Lock()
	m.gen++
	m.detachMonitorLocked()
	m.state = LoginState{Status: LoginStateIdle}
	m.mu.Unlock()

	if m.configured {
		if err := m.client.Logout(ctx); err != nil {
			return m.State(), err
		}
	}
	return m.State(), nil
}

// watch acompanha a sessão em segundo plano até conectar, falhar ou estourar o
// tempo. O contexto é próprio: o pareamento continua depois da resposta HTTP.
func (m *LoginManager) watch(generation uint64) {
	m.mu.Lock()
	stop := make(chan struct{})
	m.stop = stop
	m.mu.Unlock()

	go m.follow(generation, stop)
}

func (m *LoginManager) follow(generation uint64, stop <-chan struct{}) {
	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()
	deadline := time.NewTimer(m.timeout)
	defer deadline.Stop()

	failures := 0
	for {
		select {
		case <-stop:
			return
		case <-deadline.C:
			m.timeoutLogin(generation)
			return
		case <-ticker.C:
			state, err := m.client.LoginState(context.Background())
			if err != nil {
				failures++
				m.log.Warn("consulta da sessão do WhatsApp falhou", "scope", "whatsapp", "err", err, "falhas", failures)
				if failures >= maxLoginFailures {
					m.apply(generation, LoginState{Status: LoginStateError, Error: loginUnreachable})
					return
				}
				continue
			}
			failures = 0

			switch state.Status {
			case LoginStateConnected:
				m.apply(generation, LoginState{Status: LoginStateConnected, Message: loginConnected})
				return
			case LoginStateScanQR:
				m.apply(generation, state)
			case LoginStateError:
				m.apply(generation, state)
				return
			}
		}
	}
}

// timeoutLogin encerra o pareamento vencido: a sessão do WAHA é parada primeiro
// (para não ficar gerando QR Code em vão) e só então o painel vê `timeout`.
func (m *LoginManager) timeoutLogin(generation uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.client.CancelLogin(ctx); err != nil {
		m.log.Warn("falha ao parar a sessão do WhatsApp vencida", "scope", "whatsapp", "err", err)
	}

	m.apply(generation, LoginState{Status: LoginStateTimeout, Error: loginTimeoutMessage})
}

// apply publica o estado se este monitor ainda for o atual. Devolve false quando
// um cancelamento ou um novo pareamento tomou o lugar dele.
func (m *LoginManager) apply(generation uint64, state LoginState) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gen != generation {
		return false
	}
	m.state = state
	return true
}

// replace publica o estado sem depender de monitor (usado no caminho sem WAHA).
func (m *LoginManager) replace(state LoginState) LoginState {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = state
	return m.state
}

// detachMonitorLocked encerra o monitor em curso sem esperá-lo — ele percebe que
// foi substituído pela geração e para sozinho. Exige o mutex preso.
func (m *LoginManager) detachMonitorLocked() {
	if m.stop != nil {
		close(m.stop)
		m.stop = nil
	}
}

// encodePNGDataURL converte o texto cru do QR Code em um data URL PNG, para o
// caso de o WAHA devolver o conteúdo em vez da imagem — o painel recebe sempre
// uma imagem pronta, como no app atual.
func encodePNGDataURL(text string) (string, error) {
	code, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		return "", errors.Join(errors.New("QR Code inválido devolvido pelo WAHA"), err)
	}
	png, err := code.PNG(qrCodeSize)
	if err != nil {
		return "", errors.Join(errors.New("falha ao gerar o PNG do QR Code"), err)
	}
	return pngDataURL(png), nil
}
