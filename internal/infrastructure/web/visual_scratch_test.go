package web

import (
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// ANDAIME DE VERIFICAÇÃO VISUAL (temporário): sobe o painel inteiro sobre o
// SQLite em memória e fica no ar para a inspeção no navegador. Só roda quando
// ALUXEN_VISUAL estiver definida; é removido antes de fechar o Work.
func TestVisualScratchServer(t *testing.T) {
	if os.Getenv("ALUXEN_VISUAL") == "" {
		t.Skip("verificação visual desligada")
	}
	panel := newAgentPanel(t)
	server := httptest.NewServer(panel.handler.Router())
	defer server.Close()

	t.Logf("VISUAL URL http://%s", server.Listener.Addr().String())
	time.Sleep(20 * time.Minute)
}
