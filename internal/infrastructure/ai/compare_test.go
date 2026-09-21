package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// A asserção de compilação é a prova de que o cliente serve de implementação
// concreta da porta: sem ela o pacote não compila.
var _ application.PhotoComparator = (*Client)(nil)

// entrada é o pedido de teste: duas fotos de entrada e uma de saída, com
// checklist.
func entrada() application.PhotoComparisonInput {
	return application.PhotoComparisonInput{
		LeaseID:      "lease-1",
		TenantName:   "Maria Silva",
		PropertyName: "Rua A, 10",
		CheckIn: []application.ComparisonPhoto{
			{Name: "entrada-sala.jpg", Bytes: []byte("foto-sala-entrada"), Mime: "image/jpeg", Label: "IMG_001.jpg"},
			{Name: "entrada-cozinha.png", Bytes: []byte("foto-cozinha-entrada"), Mime: "image/png"},
		},
		CheckOut: []application.ComparisonPhoto{
			{Name: "saida-sala.jpg", Bytes: []byte("foto-sala-saida"), Mime: "image/jpeg"},
		},
		Items: []domain.InspectionItem{
			{Area: "Sala", Item: "Parede", State: "íntegra"},
			{Area: "Cozinha", Item: "Bancada", State: "íntegra", Notes: "sem trincas"},
		},
	}
}

// comparar roda a comparação contra o provedor falso com o conteúdo devolvido
// pelo "modelo".
func comparar(t *testing.T, input application.PhotoComparisonInput, content string) (application.PhotoComparison, string, error) {
	t.Helper()
	fake := newFakeProvider(t, respondContent(t, content))
	return fake.client(Config{}).CompareInspectionPhotos(context.Background(), input)
}

// respostaValida é o JSON do schema combinado, com um achado de atenção e um de
// dano.
const respostaValida = `{"summary":"Risco na parede da sala e bancada trincada.","confidence":0.83,"findings":[` +
	`{"area":"Sala","item":"Parede","state":"riscada","severity":"atencao","note":"risco superficial","checkInPhoto":"entrada-sala.jpg","checkOutPhoto":"saida-sala.jpg"},` +
	`{"area":"Cozinha","item":"Bancada","state":"trincada","severity":"dano","note":"trinca nova","checkInPhoto":"entrada-cozinha.png","checkOutPhoto":""}]}`

// ── Requisição enviada ─────────────────────────────────────────────

func TestCompareInspectionPhotosRequest(t *testing.T) {
	fake := newFakeProvider(t, respondContent(t, respostaValida))
	client := fake.client(Config{Model: "modelo-de-visao"})

	if _, _, err := client.CompareInspectionPhotos(context.Background(), entrada()); err != nil {
		t.Fatalf("comparação falhou: %v", err)
	}

	request := fake.last()
	if request.method != http.MethodPost || request.path != chatPath {
		t.Errorf("chamada = %s %s", request.method, request.path)
	}

	var payload struct {
		Model          string `json:"model"`
		ResponseFormat struct {
			Type string `json:"type"`
		} `json:"response_format"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(request.body), &payload); err != nil {
		t.Fatalf("corpo enviado não é JSON: %v", err)
	}
	if payload.Model != "modelo-de-visao" {
		t.Errorf("modelo = %q", payload.Model)
	}
	if payload.ResponseFormat.Type != "json_object" {
		t.Errorf("response_format = %q, esperado json_object", payload.ResponseFormat.Type)
	}
	if len(payload.Messages) != 2 {
		t.Fatalf("mensagens = %d, esperadas 2", len(payload.Messages))
	}
	if payload.Messages[0].Role != "system" {
		t.Errorf("papel da primeira mensagem = %q", payload.Messages[0].Role)
	}
	var prompt string
	if err := json.Unmarshal(payload.Messages[0].Content, &prompt); err != nil {
		t.Fatalf("instrução de sistema não é texto: %v", err)
	}
	if !strings.Contains(prompt, "checkInPhoto") || !strings.Contains(prompt, "ao menos uma foto") {
		t.Errorf("instrução de sistema inesperada: %q", prompt)
	}

	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(payload.Messages[1].Content, &parts); err != nil {
		t.Fatalf("conteúdo da mensagem do usuário não é lista de partes: %v", err)
	}
	// 1 instrução + (texto + imagem) por foto = 7 partes.
	if len(parts) != 7 {
		t.Fatalf("partes = %d, esperadas 7", len(parts))
	}
	if parts[0].Type != "text" || !strings.Contains(parts[0].Text, "ENTRADA") {
		t.Errorf("instrução inesperada: %+v", parts[0])
	}
	if !strings.Contains(parts[0].Text, "Sala") || !strings.Contains(parts[0].Text, "Bancada") {
		t.Errorf("instrução sem o checklist: %q", parts[0].Text)
	}
	if !strings.Contains(parts[0].Text, "Rua A, 10") {
		t.Errorf("instrução sem o imóvel: %q", parts[0].Text)
	}

	// Cada foto vai precedida do texto com série e nome, e o data URL é o
	// base64 exato do arquivo, com o mime da foto.
	want := []struct {
		label string
		url   string
	}{
		{"ENTRADA", dataURL("image/jpeg", []byte("foto-sala-entrada"))},
		{"ENTRADA", dataURL("image/png", []byte("foto-cozinha-entrada"))},
		{"SAÍDA", dataURL("image/jpeg", []byte("foto-sala-saida"))},
	}
	for i, item := range want {
		label := parts[1+2*i]
		image := parts[2+2*i]
		if label.Type != "text" || !strings.Contains(label.Text, item.label) {
			t.Errorf("rótulo %d = %+v, esperado com a série %q", i, label, item.label)
		}
		if image.Type != "image_url" || image.ImageURL.URL != item.url {
			t.Errorf("foto %d = %q, esperada %q", i, image.ImageURL.URL, item.url)
		}
	}
	if !strings.Contains(parts[1].Text, "entrada-sala.jpg") || !strings.Contains(parts[3].Text, "entrada-cozinha.png") ||
		!strings.Contains(parts[5].Text, "saida-sala.jpg") {
		t.Errorf("rótulos sem o nome exato do arquivo: %q | %q | %q", parts[1].Text, parts[3].Text, parts[5].Text)
	}
	if !strings.Contains(parts[1].Text, "IMG_001.jpg") {
		t.Errorf("rótulo sem o nome original: %q", parts[1].Text)
	}
}

func TestCompareInspectionPhotosWithoutChecklist(t *testing.T) {
	fake := newFakeProvider(t, respondContent(t, respostaValida))
	client := fake.client(Config{})

	input := entrada()
	input.Items = nil
	if _, _, err := client.CompareInspectionPhotos(context.Background(), input); err != nil {
		t.Fatalf("comparação falhou: %v", err)
	}
	if body := fake.last().body; !strings.Contains(body, "não tem checklist") {
		t.Error("instrução deveria avisar que não há checklist")
	}
}

// ── Resposta validada ──────────────────────────────────────────────

func TestCompareInspectionPhotosValid(t *testing.T) {
	comparison, raw, err := comparar(t, entrada(), respostaValida)
	if err != nil {
		t.Fatalf("comparação falhou: %v", err)
	}
	if comparison.Summary != "Risco na parede da sala e bancada trincada." {
		t.Errorf("resumo = %q", comparison.Summary)
	}
	if comparison.Confidence != 0.83 {
		t.Errorf("confiança = %v", comparison.Confidence)
	}
	if len(comparison.Findings) != 2 {
		t.Fatalf("achados = %d, esperados 2", len(comparison.Findings))
	}
	first := comparison.Findings[0]
	if first.Area != "Sala" || first.Item != "Parede" || first.State != "riscada" ||
		first.Severity != application.FindingAttention || first.Note != "risco superficial" ||
		first.CheckInPhoto != "entrada-sala.jpg" || first.CheckOutPhoto != "saida-sala.jpg" {
		t.Errorf("achado 1 = %+v", first)
	}
	second := comparison.Findings[1]
	if second.Severity != application.FindingDamage || second.CheckOutPhoto != "" {
		t.Errorf("achado 2 = %+v", second)
	}
	untouched, attention, damage := comparison.Counts()
	if untouched != 0 || attention != 1 || damage != 1 {
		t.Errorf("contagem = %d/%d/%d, esperada 0/1/1", untouched, attention, damage)
	}
	if raw != respostaValida {
		t.Errorf("resposta crua = %q, esperada a do provedor", raw)
	}
}

func TestCompareInspectionPhotosEmptyFindings(t *testing.T) {
	comparison, _, err := comparar(t, entrada(), `{"summary":"Nada mudou.","confidence":0.9,"findings":[]}`)
	if err != nil {
		t.Fatalf("comparação falhou: %v", err)
	}
	if len(comparison.Findings) != 0 {
		t.Errorf("achados = %+v, esperados nenhum", comparison.Findings)
	}
}

func TestCompareInspectionPhotosAcceptsUppercaseSeverity(t *testing.T) {
	content := `{"summary":"Ok.","confidence":0.9,"findings":[{"item":"Parede","severity":"ATENCAO"}]}`
	comparison, _, err := comparar(t, entrada(), content)
	if err != nil {
		t.Fatalf("comparação falhou: %v", err)
	}
	if comparison.Findings[0].Severity != application.FindingAttention {
		t.Errorf("severidade = %q", comparison.Findings[0].Severity)
	}
	if comparison.Findings[0].CheckInPhoto != "" || comparison.Findings[0].Area != "" {
		t.Errorf("campos não lidos deveriam ficar vazios: %+v", comparison.Findings[0])
	}
}

func TestCompareInspectionPhotosRejections(t *testing.T) {
	cases := []struct {
		name    string
		content string
		motivo  string
	}{
		{"objeto vazio", `{}`, "summary"},
		{"resumo ausente", `{"confidence":0.9,"findings":[]}`, "summary"},
		{"resumo vazio", `{"summary":"  ","confidence":0.9,"findings":[]}`, "summary"},
		{"confiança ausente", `{"summary":"Ok.","findings":[]}`, "confidence"},
		{"confiança fora da faixa", `{"summary":"Ok.","confidence":1.4,"findings":[]}`, "confidence"},
		{"achados ausentes", `{"summary":"Ok.","confidence":0.9}`, "findings"},
		{"achados nulos", `{"summary":"Ok.","confidence":0.9,"findings":null}`, "findings"},
		{"achados não são lista", `{"summary":"Ok.","confidence":0.9,"findings":"nenhum"}`, "findings"},
		{"achado sem item", `{"summary":"Ok.","confidence":0.9,"findings":[{"severity":"ok"}]}`, "item"},
		{"achado sem severidade", `{"summary":"Ok.","confidence":0.9,"findings":[{"item":"Parede"}]}`, "severity"},
		{"severidade fora do vocabulário", `{"summary":"Ok.","confidence":0.9,"findings":[{"item":"Parede","severity":"grave"}]}`, "severity"},
		{"severidade vazia", `{"summary":"Ok.","confidence":0.9,"findings":[{"item":"Parede","severity":""}]}`, "severity"},
		{"foto de entrada desconhecida", `{"summary":"Ok.","confidence":0.9,"findings":[{"item":"Parede","severity":"ok","checkInPhoto":"outra.jpg"}]}`, "checkInPhoto"},
		{"foto de saída desconhecida", `{"summary":"Ok.","confidence":0.9,"findings":[{"item":"Parede","severity":"ok","checkOutPhoto":"saida-sala.png"}]}`, "checkOutPhoto"},
		{"foto de entrada citada na saída", `{"summary":"Ok.","confidence":0.9,"findings":[{"item":"Parede","severity":"ok","checkOutPhoto":"entrada-sala.jpg"}]}`, "checkOutPhoto"},
		{"JSON inválido", `não consegui comparar as fotos`, "JSON"},
		{"JSON não é objeto", `["achado"]`, "JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comparison, raw, err := comparar(t, entrada(), tc.content)
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("erro = %v, esperado ErrInvalidResponse", err)
			}
			if !strings.Contains(err.Error(), tc.motivo) {
				t.Errorf("mensagem = %q, esperada citando %q", err.Error(), tc.motivo)
			}
			if comparison.Findings != nil || comparison.Summary != "" {
				t.Errorf("comparação = %+v, esperada vazia", comparison)
			}
			if raw != tc.content {
				t.Errorf("resposta crua = %q, esperada a do provedor para a trilha", raw)
			}
		})
	}
}

func TestCompareInspectionPhotosRejectionPointsToFinding(t *testing.T) {
	content := `{"summary":"Ok.","confidence":0.9,"findings":[` +
		`{"item":"Parede","severity":"ok"},` +
		`{"item":"Bancada","severity":"grave"}]}`
	_, _, err := comparar(t, entrada(), content)
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("erro = %v, esperado ErrInvalidResponse", err)
	}
	if !strings.Contains(err.Error(), "achado 2") {
		t.Errorf("mensagem = %q, esperada citando o achado 2", err.Error())
	}
}

// ── Validação das fotos ────────────────────────────────────────────

func TestCompareInspectionPhotosValidatesPhotos(t *testing.T) {
	cases := []struct {
		name    string
		input   func() application.PhotoComparisonInput
		config  Config
		wantErr string
	}{
		{"sem foto nenhuma", func() application.PhotoComparisonInput {
			return application.PhotoComparisonInput{LeaseID: "lease-1"}
		}, Config{}, "nenhuma foto"},
		{"foto vazia", func() application.PhotoComparisonInput {
			input := entrada()
			input.CheckOut[0].Bytes = nil
			return input
		}, Config{}, "está vazio"},
		{"mime não suportado", func() application.PhotoComparisonInput {
			input := entrada()
			input.CheckIn[0].Mime = "image/gif"
			return input
		}, Config{}, "mime"},
		{"foto acima do limite", func() application.PhotoComparisonInput {
			input := entrada()
			input.CheckOut[0].Bytes = []byte(strings.Repeat("x", 4096))
			return input
		}, Config{MaxBytes: 1024}, "acima do limite"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeProvider(t, respondContent(t, respostaValida))
			client := fake.client(tc.config)
			_, _, err := client.CompareInspectionPhotos(context.Background(), tc.input())
			if !errors.Is(err, ErrInvalidDocument) {
				t.Fatalf("erro = %v, esperado ErrInvalidDocument", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("mensagem = %q, esperada contendo %q", err.Error(), tc.wantErr)
			}
			if fake.calls() != 0 {
				t.Errorf("foto inválida não deveria chegar ao provedor (%d chamadas)", fake.calls())
			}
		})
	}
}

// ── Falhas do provedor ─────────────────────────────────────────────

func TestCompareInspectionPhotosProviderFailures(t *testing.T) {
	t.Run("HTTP 500", func(t *testing.T) {
		fake := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "modelo indisponível", http.StatusInternalServerError)
		})
		client := fake.client(Config{})
		if _, _, err := client.CompareInspectionPhotos(context.Background(), entrada()); err == nil || !strings.Contains(err.Error(), "500") {
			t.Fatalf("erro = %v, esperado com o status", err)
		}
	})

	t.Run("não configurado", func(t *testing.T) {
		fake := newFakeProvider(t, respondContent(t, respostaValida))
		client := NewClient(Config{})
		if _, _, err := client.CompareInspectionPhotos(context.Background(), entrada()); !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("erro = %v, esperado ErrNotConfigured", err)
		}
		if fake.calls() != 0 {
			t.Errorf("instância sem IA não deveria chamar o provedor (%d chamadas)", fake.calls())
		}
	})

	t.Run("resposta vazia", func(t *testing.T) {
		fake := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":""}}]}`)
		})
		client := fake.client(Config{})
		if _, _, err := client.CompareInspectionPhotos(context.Background(), entrada()); !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("erro = %v, esperado ErrInvalidResponse", err)
		}
	})
}

// A mensagem de erro vai para o log da fila: não pode carregar nome de arquivo,
// nome de locatário nem rótulo das fotos (BR-202).
func TestCompareInspectionPhotosErrorDoesNotLeak(t *testing.T) {
	content := `{"summary":"Ok.","confidence":0.9,"findings":[{"item":"Parede","severity":"grave","checkInPhoto":"entrada-sala.jpg"}]}`
	_, _, err := comparar(t, entrada(), content)
	if err == nil {
		t.Fatal("severidade inválida deveria falhar")
	}
	for _, leaked := range []string{"entrada-sala.jpg", "saida-sala.jpg", "Maria Silva", "IMG_001.jpg", "grave"} {
		if strings.Contains(err.Error(), leaked) {
			t.Errorf("mensagem %q vazou dado da vistoria (%q)", err.Error(), leaked)
		}
	}
}
