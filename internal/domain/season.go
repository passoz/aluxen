package domain

import (
	"encoding/json"
	"strings"
)

// Temporada: caução, vistoria e categorias de cobrança próprias. Tudo aditivo —
// contrato sem caução e sem vistoria continua idêntico ao de locação longa.

// DepositStatus é a situação da caução de um contrato de temporada.
type DepositStatus string

const (
	// DepositStatusNone é o contrato sem caução (caso do uso atual).
	DepositStatusNone DepositStatus = "none"
	// DepositStatusHeld é a caução recebida e retida pelo locador.
	DepositStatusHeld DepositStatus = "held"
	// DepositStatusRefunded é a caução devolvida integralmente.
	DepositStatusRefunded DepositStatus = "refunded"
	// DepositStatusPartiallyRefunded é a devolução parcial (o restante ficou retido).
	DepositStatusPartiallyRefunded DepositStatus = "partially_refunded"
	// DepositStatusForfeited é a retenção total por dano ou descumprimento.
	DepositStatusForfeited DepositStatus = "forfeited"
)

// DepositStatusLabel devolve o rótulo em português da situação da caução.
func DepositStatusLabel(status DepositStatus) string {
	switch status {
	case DepositStatusHeld:
		return "Retida"
	case DepositStatusRefunded:
		return "Devolvida"
	case DepositStatusPartiallyRefunded:
		return "Devolvida parcialmente"
	case DepositStatusForfeited:
		return "Retida integralmente"
	default:
		return "Sem caução"
	}
}

// HasDeposit informa se o contrato tem caução registrada.
func HasDeposit(depositAmount *float64, status DepositStatus) bool {
	return depositAmount != nil && *depositAmount > 0 && status != DepositStatusNone
}

// ValidateDeposit valida o registro de caução informado no contrato.
func ValidateDeposit(amount *float64, status DepositStatus) error {
	if amount == nil {
		if status != "" && status != DepositStatusNone {
			return Invalid("Informe o valor da caução.")
		}
		return nil
	}
	if *amount < 0 {
		return Invalid("O valor da caução não pode ser negativo.")
	}
	if *amount == 0 {
		return Invalid("Informe o valor da caução.")
	}
	return nil
}

// DepositRefund é o resultado do cálculo de devolução da caução.
type DepositRefund struct {
	RefundedAmount float64
	Status         DepositStatus
}

// ComputeDepositRefund calcula a devolução a partir do valor retido e do valor
// devolvido, recusando devolução maior do que o retido e devolução sem caução.
func ComputeDepositRefund(held float64, requested float64) (DepositRefund, error) {
	if held <= 0 {
		return DepositRefund{}, Invalid("Este contrato não tem caução retida para devolver.")
	}
	if requested < 0 {
		return DepositRefund{}, Invalid("O valor devolvido não pode ser negativo.")
	}
	if requested > held {
		return DepositRefund{}, Invalid("O valor devolvido não pode ser maior que a caução retida.")
	}
	if requested == 0 {
		return DepositRefund{RefundedAmount: 0, Status: DepositStatusForfeited}, nil
	}
	if requested < held {
		return DepositRefund{RefundedAmount: requested, Status: DepositStatusPartiallyRefunded}, nil
	}
	return DepositRefund{RefundedAmount: requested, Status: DepositStatusRefunded}, nil
}

// InspectionKind é o tipo de vistoria.
type InspectionKind string

const (
	InspectionKindCheckIn  InspectionKind = "checkin"
	InspectionKindCheckOut InspectionKind = "checkout"
)

// InspectionKindLabel devolve o rótulo em português do tipo de vistoria.
func InspectionKindLabel(kind InspectionKind) string {
	switch kind {
	case InspectionKindCheckIn:
		return "Vistoria de entrada"
	case InspectionKindCheckOut:
		return "Vistoria de saída"
	default:
		return string(kind)
	}
}

// Valid informa se o tipo de vistoria é aceito.
func (k InspectionKind) Valid() bool {
	return k == InspectionKindCheckIn || k == InspectionKindCheckOut
}

// InspectionCondition é o estado geral do imóvel na vistoria.
type InspectionCondition string

const (
	InspectionConditionExcellent InspectionCondition = "otima"
	InspectionConditionGood      InspectionCondition = "boa"
	InspectionConditionFair      InspectionCondition = "regular"
	InspectionConditionBad       InspectionCondition = "ruim"
)

// InspectionConditionLabel devolve o rótulo em português do estado geral.
func InspectionConditionLabel(condition InspectionCondition) string {
	switch condition {
	case InspectionConditionExcellent:
		return "Ótima"
	case InspectionConditionGood:
		return "Boa"
	case InspectionConditionFair:
		return "Regular"
	case InspectionConditionBad:
		return "Ruim"
	default:
		return string(condition)
	}
}

// Valid informa se o estado geral é aceito.
func (c InspectionCondition) Valid() bool {
	switch c {
	case InspectionConditionExcellent, InspectionConditionGood, InspectionConditionFair, InspectionConditionBad:
		return true
	default:
		return false
	}
}

// InspectionItem é um item conferido na vistoria.
type InspectionItem struct {
	Area  string `json:"area"`
	Item  string `json:"item"`
	State string `json:"state"`
	Notes string `json:"notes,omitempty"`
}

// InspectionPhoto é uma foto anexada à vistoria. `Name` é o nome do arquivo
// guardado no armazenamento da instância.
type InspectionPhoto struct {
	Name    string `json:"name"`
	Label   string `json:"label,omitempty"`
	Caption string `json:"caption,omitempty"`
}

// Inspection é o auto de vistoria de entrada ou de saída de um contrato.
type Inspection struct {
	ID           string
	LeaseID      string
	Kind         InspectionKind
	PerformedAt  string
	Condition    InspectionCondition
	Notes        *string
	Items        []InspectionItem
	Photos       []InspectionPhoto
	AuthToken    *string
	CreatedAt    string
	UpdatedAt    string
	TenantName   string
	PropertyName string
}

// EncodeInspectionItems serializa os itens para a coluna `items`.
func EncodeInspectionItems(items []InspectionItem) (*string, error) {
	return encodeJSONList(items)
}

// DecodeInspectionItems desserializa a coluna `items`.
func DecodeInspectionItems(raw *string) ([]InspectionItem, error) {
	items := []InspectionItem{}
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return items, nil
	}
	if err := json.Unmarshal([]byte(*raw), &items); err != nil {
		return nil, Invalid("Não foi possível ler os itens da vistoria.")
	}
	return items, nil
}

// EncodeInspectionPhotos serializa as fotos para a coluna `photos`.
func EncodeInspectionPhotos(photos []InspectionPhoto) (*string, error) {
	return encodeJSONList(photos)
}

// DecodeInspectionPhotos desserializa a coluna `photos`.
func DecodeInspectionPhotos(raw *string) ([]InspectionPhoto, error) {
	photos := []InspectionPhoto{}
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return photos, nil
	}
	if err := json.Unmarshal([]byte(*raw), &photos); err != nil {
		return nil, Invalid("Não foi possível ler as fotos da vistoria.")
	}
	return photos, nil
}

func encodeJSONList[T any](values []T) (*string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	text := string(encoded)
	return &text, nil
}

// ValidateInspection valida o auto de vistoria antes de gravar.
func ValidateInspection(kind InspectionKind, condition InspectionCondition, performedAt string) error {
	if !kind.Valid() {
		return Invalid("Informe o tipo da vistoria (entrada ou saída).")
	}
	if !condition.Valid() {
		return Invalid("Informe o estado geral do imóvel.")
	}
	if strings.TrimSpace(performedAt) == "" {
		return Invalid("Informe a data da vistoria.")
	}
	return nil
}
