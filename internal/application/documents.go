package application

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain"
)

// Documentos e prova pública de autenticidade.
//
// Os casos de uso deste arquivo montam os dados que o recibo, o contrato e os
// documentos da temporada (auto de vistoria e recibo de devolução de caução)
// imprimem e que a página pública de validação exibe, além de decidir quem pode
// acessar cada documento (administrador, chave de autenticidade ou o próprio
// locatário titular). Nada de PDF nem de HTTP aqui: só leitura de repositório,
// configurações e a chave de autenticidade emitida pela porta `TokenSigner`.

// Tipos de documento do token de autenticidade (`kind` do HMAC). São os mesmos
// valores do app atual: recibos e contratos já impressos continuam válidos.
const (
	documentKindReceipt = "receipt"
	documentKindLease   = "lease"
	// Os documentos da temporada usam o mesmo cálculo do HMAC, com o seu
	// próprio tipo.
	documentKindInspection = "inspection"
	documentKindDeposit    = "deposit"
)

// receiptTenantFallback e landlordFallback são os nomes usados quando o cadastro
// não traz o dado — os mesmos textos padrão do app atual.
const (
	receiptTenantFallback = "Locatário"
	landlordFallback      = "Locador"
)

// ReceiptDocumentData reúne o lançamento e os dados de apresentação do recibo:
// nomes de locatário e imóvel resolvidos pelo contrato (quando houver), endereço
// montado, configurações do locador e a chave de autenticidade do documento.
type ReceiptDocumentData struct {
	ReceiptID string
	// TenantName já vem com o padrão `Locatário` quando o cadastro está vazio,
	// como no recibo do app atual.
	TenantName string
	TenantCPF  string

	PropertyName string
	// PropertyAddress é o endereço completo (logradouro, cidade e estado).
	PropertyAddress string

	Category    domain.ReceiptCategory
	Description string

	DueDate string
	// PaidAt é vazio enquanto o lançamento não foi pago.
	PaidAt string

	Amount float64
	// PaidAmount só é significativo com HasPaidAmount (no banco,
	// `paid_amount` nulo significa pagamento integral).
	PaidAmount    float64
	HasPaidAmount bool

	// IsPaid libera o documento; IsPartial marca o pagamento parcial.
	IsPaid    bool
	IsPartial bool

	LandlordName string
	LandlordCPF  string

	// Token, AuthCode e ValidationURL formam o selo de autenticidade impresso no
	// documento e conferido pela página pública.
	Token         string
	AuthCode      string
	ValidationURL string

	// IssuedAt é o instante da emissão, usado na data do documento.
	IssuedAt time.Time

	// Label é o rótulo legível do lançamento usado na trilha de auditoria.
	Label string
}

// LeaseDocumentData reúne o contrato e os dados de apresentação do contrato em
// PDF e da página pública de validação.
type LeaseDocumentData struct {
	LeaseID      string
	TenantName   string
	TenantPhone  string
	TenantCPF    string
	PropertyName string
	// PropertyAddress é o endereço completo (logradouro, cidade e estado),
	// exibido na validação pública; PropertyStreet é o logradouro isolado, usado
	// na qualificação do imóvel dentro do contrato.
	PropertyAddress string
	PropertyStreet  string
	PropertyCity    string
	PropertyState   string
	PropertyZip     string

	StartDate  string
	EndDate    string
	DueDay     int
	RentAmount float64
	Status     domain.LeaseStatus

	LandlordName    string
	LandlordCPF     string
	LandlordAddress string

	Token         string
	AuthCode      string
	ValidationURL string

	IssuedAt time.Time

	// Label é o rótulo legível do contrato usado na trilha de auditoria.
	Label string
}

// InspectionDocumentData reúne a vistoria e os dados de apresentação do auto em
// PDF e da página pública de validação.
type InspectionDocumentData struct {
	InspectionID string
	Kind         domain.InspectionKind
	KindLabel    string

	LeaseID string
	// TenantName já vem com o padrão `Locatário` quando o cadastro está vazio.
	TenantName string
	TenantCPF  string
	// PropertyAddress é o endereço completo (logradouro, cidade e estado).
	PropertyName    string
	PropertyAddress string

	// StartDate e EndDate delimitam o período do contrato.
	StartDate string
	EndDate   string

	// PerformedAt é a data e a hora da vistoria.
	PerformedAt    string
	Condition      domain.InspectionCondition
	ConditionLabel string
	Notes          string

	Items  []domain.InspectionItem
	Photos []domain.InspectionPhoto

	LandlordName string
	LandlordCPF  string

	Token         string
	AuthCode      string
	ValidationURL string

	IssuedAt time.Time

	// Label é o rótulo legível da vistoria usado na trilha de auditoria.
	Label string
}

// DepositDocumentData reúne a caução do contrato e os dados de apresentação do
// recibo de devolução em PDF e da página pública de validação.
type DepositDocumentData struct {
	LeaseID string
	// TenantName já vem com o padrão `Locatário` quando o cadastro está vazio.
	TenantName string
	TenantCPF  string
	// PropertyAddress é o endereço completo (logradouro, cidade e estado).
	PropertyName    string
	PropertyAddress string

	LandlordName string
	LandlordCPF  string

	// Amount é a caução registrada; Refunded é o valor devolvido e Retained é o
	// que ficou com o locador.
	Amount   float64
	Refunded float64
	Retained float64

	Status      domain.DepositStatus
	StatusLabel string
	ReceivedAt  string
	// RefundedAt é vazio enquanto a caução não foi devolvida.
	RefundedAt string
	Notes      string

	// HasDeposit libera o documento: contrato sem caução não gera recibo de
	// devolução.
	HasDeposit bool

	Token         string
	AuthCode      string
	ValidationURL string

	IssuedAt time.Time

	// Label é o rótulo legível do contrato usado na trilha de auditoria.
	Label string
}

// ReceiptDocumentInput carrega o recibo com os dados de apresentação. Devolve
// `domain.ErrNotFound` quando o lançamento não existe.
func ReceiptDocumentInput(ctx context.Context, deps Deps, receiptID string) (*ReceiptDocumentData, error) {
	if strings.TrimSpace(receiptID) == "" {
		return nil, domain.ErrNotFound
	}
	receipt, err := deps.Repos.Receipts.ByID(ctx, receiptID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("carregar o recibo %s: %w", receiptID, err)
	}

	tenantID := domain.Str(receipt.TenantID)
	propertyID := domain.Str(receipt.PropertyID)
	// O contrato tem precedência sobre o vínculo direto do lançamento, como no
	// `coalesce(lease.tenant_id, receipt.tenant_id)` do app atual.
	if leaseID := domain.Str(receipt.LeaseID); leaseID != "" {
		lease, err := deps.Repos.Leases.ByID(ctx, leaseID)
		switch {
		case err == nil:
			if lease.TenantID != "" {
				tenantID = lease.TenantID
			}
			if lease.PropertyID != "" {
				propertyID = lease.PropertyID
			}
		case errors.Is(err, domain.ErrNotFound):
			// Contrato removido: o lançamento continua válido pelo próprio vínculo.
		default:
			return nil, fmt.Errorf("carregar o contrato %s do recibo: %w", leaseID, err)
		}
	}

	tenant, err := optionalTenant(ctx, deps, tenantID)
	if err != nil {
		return nil, err
	}
	property, err := optionalProperty(ctx, deps, propertyID)
	if err != nil {
		return nil, err
	}

	name, cpf, err := landlordSettings(ctx, deps)
	if err != nil {
		return nil, err
	}
	token, code, validationURL, err := documentAuth(ctx, deps, documentKindReceipt, receipt.ID)
	if err != nil {
		return nil, err
	}

	paidAmount := domain.Float(receipt.PaidAmount)
	hasPaidAmount := receipt.PaidAmount != nil
	paidAt := domain.Str(receipt.PaidAt)

	return &ReceiptDocumentData{
		ReceiptID:       receipt.ID,
		TenantName:      firstNonEmpty(receipt.TenantName, tenantName(tenant), receiptTenantFallback),
		TenantCPF:       domain.Str(tenantCpf(tenant)),
		PropertyName:    firstNonEmpty(receipt.PropertyName, propertyName(property)),
		PropertyAddress: joinAddress(property),
		Category:        receipt.Category,
		Description:     domain.Str(receipt.Description),
		DueDate:         receipt.DueDate,
		PaidAt:          paidAt,
		Amount:          receipt.Amount,
		PaidAmount:      paidAmount,
		HasPaidAmount:   hasPaidAmount,
		IsPaid:          paidAt != "",
		IsPartial:       hasPaidAmount && paidAmount > 0 && paidAmount < receipt.Amount,
		LandlordName:    name,
		LandlordCPF:     cpf,
		Token:           token,
		AuthCode:        code,
		ValidationURL:   validationURL,
		IssuedAt:        deps.Clock.Now(),
		Label:           DescribeReceipt(receipt),
	}, nil
}

// LeaseDocumentInput carrega o contrato com os dados de apresentação. Devolve
// `domain.ErrNotFound` quando o contrato não existe.
func LeaseDocumentInput(ctx context.Context, deps Deps, leaseID string) (*LeaseDocumentData, error) {
	if strings.TrimSpace(leaseID) == "" {
		return nil, domain.ErrNotFound
	}
	lease, err := deps.Repos.Leases.ByID(ctx, leaseID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("carregar o contrato %s: %w", leaseID, err)
	}

	tenant, err := optionalTenant(ctx, deps, lease.TenantID)
	if err != nil {
		return nil, err
	}
	property, err := optionalProperty(ctx, deps, lease.PropertyID)
	if err != nil {
		return nil, err
	}
	name, cpf, err := landlordSettings(ctx, deps)
	if err != nil {
		return nil, err
	}
	address, err := SettingValue(ctx, deps, domain.SettingLandlordAddress)
	if err != nil {
		return nil, err
	}
	token, code, validationURL, err := documentAuth(ctx, deps, documentKindLease, lease.ID)
	if err != nil {
		return nil, err
	}

	data := &LeaseDocumentData{
		LeaseID:         lease.ID,
		TenantName:      firstNonEmpty(lease.TenantName, tenantName(tenant), receiptTenantFallback),
		TenantPhone:     domain.Str(tenantPhone(tenant)),
		TenantCPF:       domain.Str(tenantCpf(tenant)),
		PropertyName:    firstNonEmpty(lease.PropertyName, propertyName(property)),
		PropertyAddress: joinAddress(property),
		StartDate:       lease.StartDate,
		EndDate:         lease.EndDate,
		DueDay:          lease.DueDay,
		RentAmount:      lease.RentAmount,
		Status:          lease.Status,
		LandlordName:    name,
		LandlordCPF:     cpf,
		LandlordAddress: strings.TrimSpace(address),
		Token:           token,
		AuthCode:        code,
		ValidationURL:   validationURL,
		IssuedAt:        deps.Clock.Now(),
		Label:           DescribeLease(lease),
	}
	if property != nil {
		data.PropertyStreet = strings.TrimSpace(property.Address)
		data.PropertyCity = property.City
		data.PropertyState = property.State
		data.PropertyZip = domain.Str(property.Zip)
	}
	return data, nil
}

// InspectionDocumentInput carrega o auto de vistoria com os dados de
// apresentação. Devolve `domain.ErrNotFound` quando a vistoria não existe.
func InspectionDocumentInput(ctx context.Context, deps Deps, id string) (*InspectionDocumentData, error) {
	if strings.TrimSpace(id) == "" {
		return nil, domain.ErrNotFound
	}
	inspection, err := deps.Repos.Inspections.ByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("carregar a vistoria %s: %w", id, err)
	}

	lease, err := optionalLease(ctx, deps, inspection.LeaseID)
	if err != nil {
		return nil, err
	}
	tenant, err := optionalTenant(ctx, deps, leaseTenantID(lease))
	if err != nil {
		return nil, err
	}
	property, err := optionalProperty(ctx, deps, leasePropertyID(lease))
	if err != nil {
		return nil, err
	}
	name, cpf, err := landlordSettings(ctx, deps)
	if err != nil {
		return nil, err
	}
	token, code, validationURL, err := documentAuth(ctx, deps, documentKindInspection, inspection.ID)
	if err != nil {
		return nil, err
	}

	// Os nomes vêm da própria vistoria (o repositório já traz o contrato), do
	// cadastro como reforço e, por último, do padrão do documento.
	resolvedTenant := firstNonEmpty(inspection.TenantName, leaseTenantName(lease), tenantName(tenant), receiptTenantFallback)
	resolvedProperty := firstNonEmpty(inspection.PropertyName, leasePropertyName(lease), propertyName(property))

	data := &InspectionDocumentData{
		InspectionID:    inspection.ID,
		Kind:            inspection.Kind,
		KindLabel:       domain.InspectionKindLabel(inspection.Kind),
		LeaseID:         inspection.LeaseID,
		TenantName:      resolvedTenant,
		TenantCPF:       domain.Str(tenantCpf(tenant)),
		PropertyName:    resolvedProperty,
		PropertyAddress: joinAddress(property),
		StartDate:       leaseStartDate(lease),
		EndDate:         leaseEndDate(lease),
		PerformedAt:     inspection.PerformedAt,
		Condition:       inspection.Condition,
		ConditionLabel:  domain.InspectionConditionLabel(inspection.Condition),
		Notes:           domain.Str(inspection.Notes),
		Items:           inspection.Items,
		Photos:          inspection.Photos,
		LandlordName:    name,
		LandlordCPF:     cpf,
		Token:           token,
		AuthCode:        code,
		ValidationURL:   validationURL,
		IssuedAt:        deps.Clock.Now(),
		Label:           DescribeLease(leaseLabelSource(lease, resolvedTenant, resolvedProperty)),
	}
	return data, nil
}

// DepositDocumentInput carrega a caução do contrato com os dados de
// apresentação. Devolve `domain.ErrNotFound` quando o contrato não existe.
func DepositDocumentInput(ctx context.Context, deps Deps, leaseID string) (*DepositDocumentData, error) {
	if strings.TrimSpace(leaseID) == "" {
		return nil, domain.ErrNotFound
	}
	lease, err := deps.Repos.Leases.ByID(ctx, leaseID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("carregar o contrato %s: %w", leaseID, err)
	}

	deposit := DepositViewOf(lease)
	tenant, err := optionalTenant(ctx, deps, lease.TenantID)
	if err != nil {
		return nil, err
	}
	property, err := optionalProperty(ctx, deps, lease.PropertyID)
	if err != nil {
		return nil, err
	}
	name, cpf, err := landlordSettings(ctx, deps)
	if err != nil {
		return nil, err
	}
	token, code, validationURL, err := documentAuth(ctx, deps, documentKindDeposit, lease.ID)
	if err != nil {
		return nil, err
	}

	return &DepositDocumentData{
		LeaseID:         lease.ID,
		TenantName:      firstNonEmpty(lease.TenantName, tenantName(tenant), receiptTenantFallback),
		TenantCPF:       domain.Str(tenantCpf(tenant)),
		PropertyName:    firstNonEmpty(lease.PropertyName, propertyName(property)),
		PropertyAddress: joinAddress(property),
		LandlordName:    name,
		LandlordCPF:     cpf,
		Amount:          deposit.Amount,
		Refunded:        deposit.Refunded,
		Retained:        deposit.Outstanding,
		Status:          deposit.Status,
		StatusLabel:     domain.DepositStatusLabel(deposit.Status),
		ReceivedAt:      deposit.ReceivedAt,
		RefundedAt:      deposit.RefundedAt,
		Notes:           deposit.Notes,
		HasDeposit:      domain.HasDeposit(lease.DepositAmount, deposit.Status),
		Token:           token,
		AuthCode:        code,
		ValidationURL:   validationURL,
		IssuedAt:        deps.Clock.Now(),
		Label:           DescribeLease(lease),
	}, nil
}

// VerifyReceiptToken valida a chave de autenticidade e devolve o recibo. Chave
// inválida vira erro de validação com a mensagem do app atual; recibo
// inexistente vira `domain.ErrNotFound`. Recibo não pago é recusado pela rota,
// não aqui: o documento existe e a chave confere.
func VerifyReceiptToken(ctx context.Context, deps Deps, id, token string) (*ReceiptDocumentData, error) {
	if !documentTokenMatches(deps, documentKindReceipt, id, token) {
		return nil, invalidAuthenticityKey()
	}
	return ReceiptDocumentInput(ctx, deps, id)
}

// VerifyLeaseToken valida a chave de autenticidade e devolve o contrato.
func VerifyLeaseToken(ctx context.Context, deps Deps, id, token string) (*LeaseDocumentData, error) {
	if !documentTokenMatches(deps, documentKindLease, id, token) {
		return nil, invalidAuthenticityKey()
	}
	return LeaseDocumentInput(ctx, deps, id)
}

// VerifyInspectionToken valida a chave de autenticidade e devolve o auto de
// vistoria. Chave inválida vira erro de validação com a mensagem do app atual;
// vistoria inexistente vira `domain.ErrNotFound`.
func VerifyInspectionToken(ctx context.Context, deps Deps, id, token string) (*InspectionDocumentData, error) {
	if !documentTokenMatches(deps, documentKindInspection, id, token) {
		return nil, invalidAuthenticityKey()
	}
	return InspectionDocumentInput(ctx, deps, id)
}

// VerifyDepositToken valida a chave de autenticidade e devolve o recibo de
// devolução de caução.
func VerifyDepositToken(ctx context.Context, deps Deps, leaseID, token string) (*DepositDocumentData, error) {
	if !documentTokenMatches(deps, documentKindDeposit, leaseID, token) {
		return nil, invalidAuthenticityKey()
	}
	return DepositDocumentInput(ctx, deps, leaseID)
}

// invalidAuthenticityKey é a mensagem do app atual para chave que não confere.
func invalidAuthenticityKey() error {
	return domain.Invalid("Chave de autenticação inválida ou documento não autenticado.")
}

// CanAccessReceipt decide se o solicitante pode ver o recibo: o administrador
// sempre pode; o locatário só quando é o titular do recibo (pelo contrato, ou
// pelo vínculo direto do lançamento). Recibo inexistente não pertence a ninguém.
func CanAccessReceipt(ctx context.Context, deps Deps, receiptID, adminID, clientTenantID string) (bool, error) {
	if adminID != "" {
		return true, nil
	}
	if clientTenantID == "" || strings.TrimSpace(receiptID) == "" {
		return false, nil
	}
	owner, err := receiptOwnerTenantID(ctx, deps, receiptID)
	if err != nil {
		return false, err
	}
	return owner != "" && owner == clientTenantID, nil
}

// CanAccessLease decide se o solicitante pode ver o contrato. Vale a mesma
// regra do recibo: administrador sempre; locatário só o titular.
func CanAccessLease(ctx context.Context, deps Deps, leaseID, adminID, clientTenantID string) (bool, error) {
	if adminID != "" {
		return true, nil
	}
	if clientTenantID == "" || strings.TrimSpace(leaseID) == "" {
		return false, nil
	}
	return leaseTenantMatches(ctx, deps, leaseID, clientTenantID)
}

// CanAccessDeposit decide se o solicitante pode ver o recibo de devolução de
// caução. Vale a regra do contrato: administrador sempre; locatário só o
// titular. Contrato inexistente não pertence a ninguém.
func CanAccessDeposit(ctx context.Context, deps Deps, leaseID, adminID, clientTenantID string) (bool, error) {
	if adminID != "" {
		return true, nil
	}
	if clientTenantID == "" || strings.TrimSpace(leaseID) == "" {
		return false, nil
	}
	return leaseTenantMatches(ctx, deps, leaseID, clientTenantID)
}

// CanAccessInspection decide se o solicitante pode ver o auto de vistoria:
// administrador sempre; locatário só quando é o titular do contrato da vistoria.
// Vistoria inexistente não pertence a ninguém.
func CanAccessInspection(ctx context.Context, deps Deps, inspectionID, adminID, clientTenantID string) (bool, error) {
	if adminID != "" {
		return true, nil
	}
	if clientTenantID == "" || strings.TrimSpace(inspectionID) == "" {
		return false, nil
	}
	inspection, err := deps.Repos.Inspections.ByID(ctx, inspectionID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("carregar a vistoria %s: %w", inspectionID, err)
	}
	return leaseTenantMatches(ctx, deps, inspection.LeaseID, clientTenantID)
}

// leaseTenantMatches informa se o contrato pertence ao locatário informado.
func leaseTenantMatches(ctx context.Context, deps Deps, leaseID, tenantID string) (bool, error) {
	lease, err := deps.Repos.Leases.ByID(ctx, leaseID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("carregar o contrato %s: %w", leaseID, err)
	}
	return lease.TenantID != "" && lease.TenantID == tenantID, nil
}

// receiptOwnerTenantID devolve o locatário titular do recibo (o do contrato tem
// precedência sobre o vínculo direto), ou string vazia quando não há titular ou
// o recibo não existe.
func receiptOwnerTenantID(ctx context.Context, deps Deps, receiptID string) (string, error) {
	receipt, err := deps.Repos.Receipts.ByID(ctx, receiptID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("carregar o recibo %s: %w", receiptID, err)
	}
	if leaseID := domain.Str(receipt.LeaseID); leaseID != "" {
		lease, err := deps.Repos.Leases.ByID(ctx, leaseID)
		switch {
		case err == nil:
			if lease.TenantID != "" {
				return lease.TenantID, nil
			}
		case errors.Is(err, domain.ErrNotFound):
		default:
			return "", fmt.Errorf("carregar o contrato %s do recibo: %w", leaseID, err)
		}
	}
	return domain.Str(receipt.TenantID), nil
}

// documentTokenMatches confere a chave do documento em tempo constante. O token
// esperado vem da mesma porta que o emite (`TokenSigner`), então o algoritmo do
// HMAC continua em um só lugar; aqui fica apenas a comparação, que precisa ser
// imune a diferença de tempo — como no `authz.Verify` da borda HTTP.
func documentTokenMatches(deps Deps, kind, id, token string) bool {
	if deps.Signer == nil || token == "" || strings.TrimSpace(id) == "" {
		return false
	}
	expected := deps.Signer.Generate(kind, id)
	if len(token) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1
}

// documentAuth emite o token, a chave legível e a URL pública de validação.
//
// A base pública sai das configurações/ambiente (`app_url`, `APP_URL`,
// `BASE_URL`, produção padrão): o QR é lido fora da requisição que emitiu o
// documento, então o endereço precisa ser o configurado na instância.
func documentAuth(ctx context.Context, deps Deps, kind, id string) (string, string, string, error) {
	if deps.Signer == nil {
		return "", "", "", fmt.Errorf("emissor de tokens de autenticidade não configurado")
	}
	token := deps.Signer.Generate(kind, id)
	base := ResolvePublicBaseURL(ctx, deps, "")
	return token, deps.Signer.FormatCode(token), deps.Signer.ValidationURL(base, kind, id, token), nil
}

// landlordSettings devolve o nome e o CPF do locador, com os padrões do app.
func landlordSettings(ctx context.Context, deps Deps) (string, string, error) {
	name, err := SettingValue(ctx, deps, domain.SettingLandlordName)
	if err != nil {
		return "", "", err
	}
	cpf, err := SettingValue(ctx, deps, domain.SettingLandlordCPF)
	if err != nil {
		return "", "", err
	}
	return firstNonEmpty(strings.TrimSpace(name), landlordFallback), strings.TrimSpace(cpf), nil
}

// optionalTenant carrega o locatário, tratando ausência como cadastro
// incompleto (o documento continua sendo emitido com o que existe).
func optionalTenant(ctx context.Context, deps Deps, tenantID string) (*domain.Tenant, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, nil
	}
	tenant, err := deps.Repos.Tenants.ByID(ctx, tenantID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("carregar o locatário %s: %w", tenantID, err)
	}
	return tenant, nil
}

// optionalProperty carrega o imóvel com o mesmo tratamento.
func optionalProperty(ctx context.Context, deps Deps, propertyID string) (*domain.Property, error) {
	if strings.TrimSpace(propertyID) == "" {
		return nil, nil
	}
	property, err := deps.Repos.Properties.ByID(ctx, propertyID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("carregar o imóvel %s: %w", propertyID, err)
	}
	return property, nil
}

// joinAddress monta o endereço completo do imóvel (logradouro, cidade e
// estado), como o app atual faz antes de imprimir e de exibir o documento.
func joinAddress(property *domain.Property) string {
	if property == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	for _, part := range []string{property.Address, property.City, property.State} {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, ", ")
}

// optionalLease carrega o contrato tratando ausência como documento sem
// contrato: a vistoria continua sendo emitida com o que existe no cadastro.
func optionalLease(ctx context.Context, deps Deps, leaseID string) (*domain.Lease, error) {
	if strings.TrimSpace(leaseID) == "" {
		return nil, nil
	}
	lease, err := deps.Repos.Leases.ByID(ctx, leaseID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("carregar o contrato %s: %w", leaseID, err)
	}
	return lease, nil
}

// leaseTenantID, leasePropertyID, leaseTenantName, leasePropertyName,
// leaseStartDate e leaseEndDate leem campos de um contrato possivelmente
// ausente.
func leaseTenantID(lease *domain.Lease) string {
	if lease == nil {
		return ""
	}
	return lease.TenantID
}

func leasePropertyID(lease *domain.Lease) string {
	if lease == nil {
		return ""
	}
	return lease.PropertyID
}

func leaseTenantName(lease *domain.Lease) string {
	if lease == nil {
		return ""
	}
	return lease.TenantName
}

func leasePropertyName(lease *domain.Lease) string {
	if lease == nil {
		return ""
	}
	return lease.PropertyName
}

func leaseStartDate(lease *domain.Lease) string {
	if lease == nil {
		return ""
	}
	return lease.StartDate
}

func leaseEndDate(lease *domain.Lease) string {
	if lease == nil {
		return ""
	}
	return lease.EndDate
}

// leaseLabelSource devolve o contrato para o rótulo da trilha ou, quando a
// vistoria não tem contrato (ou o contrato não traz os nomes), um contrato
// sintético apenas com os nomes resolvidos pelo documento.
func leaseLabelSource(lease *domain.Lease, tenantName, propertyName string) *domain.Lease {
	if lease != nil && (lease.TenantName != "" || lease.PropertyName != "") {
		return lease
	}
	return &domain.Lease{TenantName: tenantName, PropertyName: propertyName}
}

// tenantName, tenantCpf e tenantPhone leem campos de um locatário possivelmente
// ausente.
func tenantName(tenant *domain.Tenant) string {
	if tenant == nil {
		return ""
	}
	return tenant.Name
}

func tenantCpf(tenant *domain.Tenant) *string {
	if tenant == nil {
		return nil
	}
	return tenant.CPF
}

func tenantPhone(tenant *domain.Tenant) *string {
	if tenant == nil {
		return nil
	}
	return tenant.Phone
}

// propertyName lê o nome de um imóvel possivelmente ausente.
func propertyName(property *domain.Property) string {
	if property == nil {
		return ""
	}
	return property.Name
}

// firstNonEmpty devolve o primeiro valor não vazio depois de aparado.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
