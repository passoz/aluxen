package web

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Temporada: caução, vistoria e documentos próprios.
//
//	POST /dashboard/deposit     — intents deposit.set e deposit.refund
//	POST /dashboard/inspection  — intents inspection.create, inspection.update e inspection.delete
//
// Os documentos da temporada (auto de vistoria e recibo de devolução) ficam em
// handlers_season_documents.go.

const (
	maxPhotoBytes  = 8 << 20  // 8 MiB por foto
	maxPhotos      = 10       // fotos por vistoria
	maxUploadBytes = 40 << 20 // teto do corpo multipart
)

// DepositMutation registra a caução de um contrato ou devolve o valor retido.
func (h *Handlers) DepositMutation(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Não foi possível ler os dados enviados."})
		return
	}
	actx := h.auditContext(r, domain.AuditOriginWeb)
	_ = user

	var err error
	switch strings.TrimSpace(r.FormValue("intent")) {
	case "deposit.set":
		amount, parseErr := parseMoneyField(r.FormValue("deposit_amount"))
		if parseErr != nil {
			err = parseErr
			break
		}
		_, err = application.SetLeaseDeposit(r.Context(), h.Deps, actx, application.DepositInput{
			LeaseID:    r.FormValue("lease_id"),
			Amount:     amount,
			ReceivedAt: strings.TrimSpace(r.FormValue("deposit_received_at")),
			Notes:      r.FormValue("deposit_notes"),
		})

	case "deposit.refund":
		requested, parseErr := parseMoneyField(r.FormValue("refund_amount"))
		if parseErr != nil {
			err = parseErr
			break
		}
		_, err = application.RefundDeposit(r.Context(), h.Deps, actx, r.FormValue("lease_id"), requested, r.FormValue("refund_notes"))

	default:
		err = domain.Invalid("Ação inválida.")
	}

	if err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// InspectionMutation cria, altera ou exclui um auto de vistoria.
func (h *Handlers) InspectionMutation(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Não foi possível ler os dados enviados."})
		return
	}
	actx := h.auditContext(r, domain.AuditOriginWeb)
	_ = user

	intent := strings.TrimSpace(r.FormValue("intent"))
	if intent == "inspection.delete" {
		if err := application.InspectionDelete(r.Context(), h.Deps, actx, r.FormValue("id")); err != nil {
			h.mapError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	photos, err := h.storeInspectionPhotos(r)
	if err != nil {
		h.mapError(w, r, err)
		return
	}

	input := application.InspectionInput{
		ID:          r.FormValue("id"),
		LeaseID:     r.FormValue("lease_id"),
		Kind:        domain.InspectionKind(strings.TrimSpace(r.FormValue("kind"))),
		PerformedAt: strings.TrimSpace(r.FormValue("performed_at")),
		Condition:   domain.InspectionCondition(strings.TrimSpace(r.FormValue("condition"))),
		Notes:       r.FormValue("notes"),
		Items:       inspectionItemsFrom(r),
		Photos:      photos,
	}

	switch intent {
	case "inspection.create":
		_, err = application.InspectionCreate(r.Context(), h.Deps, actx, input)
	case "inspection.update":
		_, err = application.InspectionUpdate(r.Context(), h.Deps, actx, input)
	default:
		err = domain.Invalid("Ação inválida.")
	}
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// storeInspectionPhotos guarda as fotos enviadas no armazenamento da instância.
func (h *Handlers) storeInspectionPhotos(r *http.Request) ([]domain.InspectionPhoto, error) {
	if h.Deps.Attachments == nil {
		return nil, nil
	}
	files := r.MultipartForm.File["photos"]
	if len(files) > maxPhotos {
		return nil, domain.Invalid("Envie no máximo %d fotos por vistoria.", maxPhotos)
	}

	photos := make([]domain.InspectionPhoto, 0, len(files))
	for _, header := range files {
		if header.Size > maxPhotoBytes {
			return nil, domain.Invalid("Cada foto pode ter no máximo 8 MB.")
		}
		file, err := header.Open()
		if err != nil {
			return nil, domain.Invalid("Não foi possível ler a foto enviada.")
		}
		content, err := io.ReadAll(io.LimitReader(file, maxPhotoBytes))
		closeErr := file.Close()
		if err != nil {
			return nil, domain.Invalid("Não foi possível ler a foto enviada.")
		}
		if closeErr != nil {
			h.Log.Warn("foto de vistoria não fechada corretamente", "scope", "http", "err", closeErr)
		}
		if !isSupportedPhoto(header.Header.Get("Content-Type"), header.Filename) {
			return nil, domain.Invalid("Formato de foto não suportado. Use JPG, PNG ou WEBP.")
		}

		name, err := h.Deps.Attachments.Save("inspections", header.Filename, content)
		if err != nil {
			return nil, err
		}
		photos = append(photos, domain.InspectionPhoto{Name: name, Label: header.Filename})
	}
	return photos, nil
}

// isSupportedPhoto aceita as extensões e tipos que o navegador envia de celular.
func isSupportedPhoto(contentType, filename string) bool {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/jpeg", "image/png", "image/webp", "image/heic", "image/heif", "":
	default:
		return false
	}
	lower := strings.ToLower(filename)
	for _, extension := range []string{".jpg", ".jpeg", ".png", ".webp", ".heic", ".heif"} {
		if strings.HasSuffix(lower, extension) {
			return true
		}
	}
	return strings.TrimSpace(contentType) != ""
}

// inspectionItemsFrom lê os itens enviados como campos repetidos paralelos.
func inspectionItemsFrom(r *http.Request) []domain.InspectionItem {
	areas := r.Form["item_area"]
	names := r.Form["item_name"]
	states := r.Form["item_state"]
	notes := r.Form["item_notes"]

	items := make([]domain.InspectionItem, 0, len(names))
	for index, name := range names {
		if strings.TrimSpace(name) == "" {
			continue
		}
		item := domain.InspectionItem{Item: strings.TrimSpace(name)}
		if index < len(areas) {
			item.Area = strings.TrimSpace(areas[index])
		}
		if index < len(states) {
			item.State = strings.TrimSpace(states[index])
		}
		if index < len(notes) {
			item.Notes = strings.TrimSpace(notes[index])
		}
		items = append(items, item)
	}
	return items
}

// parseMoneyField aceita `2500.00` (formato do painel) e a máscara pt-BR.
func parseMoneyField(raw string) (float64, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, domain.Invalid("Informe o valor.")
	}
	normalized := strings.ReplaceAll(value, "R$", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	if strings.Contains(normalized, ",") {
		normalized = strings.ReplaceAll(normalized, ".", "")
		normalized = strings.ReplaceAll(normalized, ",", ".")
	}
	amount, err := strconv.ParseFloat(normalized, 64)
	if err != nil {
		return 0, domain.Invalid("Informe um valor válido.")
	}
	return amount, nil
}
