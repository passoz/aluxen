package application

import (
	"context"
	"fmt"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Área do Cliente (portal do locatário).
//
// Somente leitura e isolada por titular: contratos, recibos quitados e
// documentos vêm sempre do locatário autenticado — o portal não expõe nada de
// outro cadastro. A única escrita do locatário é a troca da própria senha, em
// auth.go; nenhuma regra de negócio de locação passa por aqui.

// ClientLeaseView é um contrato do locatário na Área do Cliente, com os valores
// já formatados para exibição e os links do documento e da prova pública.
type ClientLeaseView struct {
	ID              string
	PropertyName    string
	PropertyAddress string
	// Period é a vigência legível (`01/03/2026 a 28/02/2027`).
	Period      string
	DueDay      int
	RentAmount  string
	Status      string
	StatusLabel string
	// StatusClass é a classe do badge do design system correspondente à
	// situação: ativo/pago, a vencer, parcial ou encerrado.
	StatusClass string
	// PDFURL baixa o contrato; ValidationURL abre a página pública com a chave
	// de autenticidade do documento.
	PDFURL        string
	ValidationURL string
}

// ClientReceiptView é um recibo quitado do locatário (integral ou parcial).
type ClientReceiptView struct {
	ID string
	// Reference é a competência ou a descrição do lançamento (`Aluguel 09/2026`).
	Reference     string
	PropertyName  string
	DueDate       string
	PaidAt        string
	AmountPaid    string
	IsPartial     bool
	PDFURL        string
	ValidationURL string
}

// ClientArea é tudo que o locatário autenticado pode consultar.
type ClientArea struct {
	Tenant   *domain.Tenant
	Leases   []ClientLeaseView
	Receipts []ClientReceiptView
}

// LoadClientArea carrega a Área do Cliente do titular. Devolve
// `domain.ErrUnauthorized` quando o locatário não existe mais.
func LoadClientArea(ctx context.Context, deps Deps, tenantID string) (*ClientArea, error) {
	tenant, err := ClientByID(ctx, deps, tenantID)
	if err != nil {
		return nil, err
	}
	leases, err := clientLeaseViews(ctx, deps, tenant.ID)
	if err != nil {
		return nil, err
	}
	receipts, err := clientReceiptViews(ctx, deps, tenant.ID)
	if err != nil {
		return nil, err
	}
	return &ClientArea{Tenant: tenant, Leases: leases, Receipts: receipts}, nil
}

// clientLeaseViews lista os contratos do titular com o endereço do imóvel e os
// links do documento. A ordem é a do repositório (contrato mais recente antes).
func clientLeaseViews(ctx context.Context, deps Deps, tenantID string) ([]ClientLeaseView, error) {
	leases, err := deps.Repos.Leases.ByTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("listar contratos do locatário: %w", err)
	}

	views := make([]ClientLeaseView, 0, len(leases))
	addresses := make(map[string]string, len(leases))
	for _, lease := range leases {
		address, err := clientPropertyAddress(ctx, deps, lease.PropertyID, addresses)
		if err != nil {
			return nil, err
		}
		validationURL, err := clientValidationURL(ctx, deps, documentKindLease, lease.ID)
		if err != nil {
			return nil, err
		}
		views = append(views, ClientLeaseView{
			ID:              lease.ID,
			PropertyName:    lease.PropertyName,
			PropertyAddress: address,
			Period:          format.FormatDateBR(lease.StartDate) + " a " + format.FormatDateBR(lease.EndDate),
			DueDay:          lease.DueDay,
			RentAmount:      format.FormatCurrency(lease.RentAmount),
			Status:          string(lease.Status),
			StatusLabel:     domain.LeaseStatusLabel(lease.Status),
			StatusClass:     clientLeaseStatusClass(lease.Status),
			PDFURL:          "/contract-pdf?id=" + lease.ID,
			ValidationURL:   validationURL,
		})
	}
	return views, nil
}

// clientReceiptViews lista os recibos quitados do titular — o portal só mostra o
// que foi pago, integral ou parcialmente. A ordem é a do repositório (vencimento
// mais recente antes).
func clientReceiptViews(ctx context.Context, deps Deps, tenantID string) ([]ClientReceiptView, error) {
	receipts, err := deps.Repos.Receipts.ByTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("listar recibos do locatário: %w", err)
	}

	views := make([]ClientReceiptView, 0, len(receipts))
	for _, receipt := range receipts {
		paidAt := domain.Str(receipt.PaidAt)
		if paidAt == "" {
			continue
		}
		// Sem valor pago explícito o pagamento é integral, como nas demais telas.
		paidAmount := receipt.Amount
		if receipt.PaidAmount != nil {
			paidAmount = *receipt.PaidAmount
		}
		validationURL, err := clientValidationURL(ctx, deps, documentKindReceipt, receipt.ID)
		if err != nil {
			return nil, err
		}
		views = append(views, ClientReceiptView{
			ID:            receipt.ID,
			Reference:     clientReceiptReference(receipt),
			PropertyName:  receipt.PropertyName,
			DueDate:       format.FormatDateBR(receipt.DueDate),
			PaidAt:        format.FormatDateTimeBR(paidAt),
			AmountPaid:    format.FormatCurrency(paidAmount),
			IsPartial:     paidAmount > 0 && paidAmount < receipt.Amount,
			PDFURL:        "/pdf?id=" + receipt.ID,
			ValidationURL: validationURL,
		})
	}
	return views, nil
}

// clientLeaseStatusClass traduz a situação do contrato para a classe do badge
// usada na Área do Cliente, como no app atual.
func clientLeaseStatusClass(status domain.LeaseStatus) string {
	switch status {
	case domain.LeaseStatusActive:
		return "paid"
	case domain.LeaseStatusSeason:
		return "partial"
	case domain.LeaseStatusTerminated:
		return "terminated"
	default:
		return "due"
	}
}

// clientReceiptReference monta a referência do recibo exibida ao locatário: o
// lançamento avulso usa a descrição e o aluguel usa a competência do vencimento
// (`Aluguel 09/2026`), como no app atual.
func clientReceiptReference(receipt domain.Receipt) string {
	if receipt.Category == domain.ReceiptCategoryExtra {
		return firstNonEmpty(domain.Str(receipt.Description), "Cobrança avulsa")
	}
	month := ""
	if len(receipt.DueDate) >= 7 {
		month = receipt.DueDate[5:7] + "/" + receipt.DueDate[0:4]
	}
	return firstNonEmpty(domain.ReceiptCategoryLabel(receipt.Category), "Aluguel") + " " + month
}

// clientPropertyAddress resolve o endereço do imóvel do contrato uma única vez
// por imóvel: o cache evita reler o mesmo cadastro em contratos repetidos.
func clientPropertyAddress(ctx context.Context, deps Deps, propertyID string, cache map[string]string) (string, error) {
	if address, ok := cache[propertyID]; ok {
		return address, nil
	}
	property, err := optionalProperty(ctx, deps, propertyID)
	if err != nil {
		return "", err
	}
	address := joinAddress(property)
	cache[propertyID] = address
	return address, nil
}

// clientValidationURL monta o link público de validação do documento do titular
// com a chave de autenticidade da instância (a mesma emissão do PDF e da página
// pública de validação).
func clientValidationURL(ctx context.Context, deps Deps, kind, id string) (string, error) {
	_, _, validationURL, err := documentAuth(ctx, deps, kind, id)
	if err != nil {
		return "", fmt.Errorf("montar o link de validação do documento %s: %w", id, err)
	}
	return validationURL, nil
}
