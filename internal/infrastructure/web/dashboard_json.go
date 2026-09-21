package web

import (
	"time"

	"github.com/passoz/aluxen/internal/domain"
)

// DTOs do modo JSON do painel (`GET /dashboard?json=true`). As chaves repetem
// exatamente as do app atual, que o cliente antigo consumia: snake_case nos
// campos do banco e camelCase só onde o original já usava (nada renomeado).
//
// O domínio fica sem tag de JSON de propósito: a apresentação mora na borda.

type tenantJSON struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Email             string  `json:"email"`
	Phone             *string `json:"phone"`
	CPF               *string `json:"cpf"`
	PasswordChangedAt *string `json:"password_changed_at,omitempty"`
}

type propertyJSON struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Address string  `json:"address"`
	City    string  `json:"city"`
	State   string  `json:"state"`
	Zip     *string `json:"zip"`
}

type leaseJSON struct {
	ID            string   `json:"id"`
	TenantID      string   `json:"tenant_id"`
	PropertyID    string   `json:"property_id"`
	StartDate     string   `json:"start_date"`
	EndDate       string   `json:"end_date"`
	DueDay        int      `json:"due_day"`
	RentAmount    float64  `json:"rent_amount"`
	Status        string   `json:"status"`
	TenantName    string   `json:"tenant_name"`
	PropertyName  string   `json:"property_name"`
	DepositAmount *float64 `json:"deposit_amount,omitempty"`
	DepositStatus string   `json:"deposit_status,omitempty"`
}

type receiptJSON struct {
	ID           string   `json:"id"`
	LeaseID      *string  `json:"lease_id"`
	TenantID     *string  `json:"tenant_id"`
	PropertyID   *string  `json:"property_id"`
	Category     string   `json:"category"`
	Description  *string  `json:"description"`
	DueDate      string   `json:"due_date"`
	PaidAt       *string  `json:"paid_at"`
	Amount       float64  `json:"amount"`
	PaidAmount   *float64 `json:"paid_amount"`
	GeneratedKey *string  `json:"generated_key"`
	TenantName   string   `json:"tenant_name"`
	PropertyName string   `json:"property_name"`
	Status       string   `json:"status"`
	StatusLabel  string   `json:"status_label"`
}

func tenantsJSON(tenants []domain.Tenant) []tenantJSON {
	out := make([]tenantJSON, 0, len(tenants))
	for _, tenant := range tenants {
		out = append(out, tenantJSON{
			ID: tenant.ID, Name: tenant.Name, Email: tenant.Email,
			Phone: tenant.Phone, CPF: tenant.CPF, PasswordChangedAt: tenant.PasswordChangedAt,
		})
	}
	return out
}

func propertiesJSON(properties []domain.Property) []propertyJSON {
	out := make([]propertyJSON, 0, len(properties))
	for _, property := range properties {
		out = append(out, propertyJSON{
			ID: property.ID, Name: property.Name, Address: property.Address,
			City: property.City, State: property.State, Zip: property.Zip,
		})
	}
	return out
}

func leasesJSON(leases []domain.Lease) []leaseJSON {
	out := make([]leaseJSON, 0, len(leases))
	for _, lease := range leases {
		// Caução só aparece quando existe: contrato sem caução responde igual ao
		// app antigo, que não tinha esses campos.
		depositStatus := ""
		if domain.HasDeposit(lease.DepositAmount, lease.DepositStatus) {
			depositStatus = string(lease.DepositStatus)
		}
		out = append(out, leaseJSON{
			ID: lease.ID, TenantID: lease.TenantID, PropertyID: lease.PropertyID,
			StartDate: lease.StartDate, EndDate: lease.EndDate, DueDay: lease.DueDay,
			RentAmount: lease.RentAmount, Status: string(lease.Status),
			TenantName: lease.TenantName, PropertyName: lease.PropertyName,
			DepositAmount: lease.DepositAmount, DepositStatus: depositStatus,
		})
	}
	return out
}

func receiptsJSON(receipts []domain.Receipt) []receiptJSON {
	out := make([]receiptJSON, 0, len(receipts))
	for _, receipt := range receipts {
		status := domain.ReceiptStatusOf(receipt, time.Now())
		out = append(out, receiptJSON{
			ID: receipt.ID, LeaseID: receipt.LeaseID, TenantID: receipt.TenantID, PropertyID: receipt.PropertyID,
			Category: string(receipt.Category), Description: receipt.Description, DueDate: receipt.DueDate,
			PaidAt: receipt.PaidAt, Amount: receipt.Amount, PaidAmount: receipt.PaidAmount,
			GeneratedKey: receipt.GeneratedKey, TenantName: receipt.TenantName, PropertyName: receipt.PropertyName,
			Status: string(status), StatusLabel: domain.ReceiptStatusLabel(status),
		})
	}
	return out
}
