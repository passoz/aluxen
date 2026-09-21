package application

import (
	"context"
	"fmt"

	"github.com/passoz/aluxen/internal/domain"
)

// Leituras usadas pelo painel, pelo modo JSON e pelos indicadores.

// DashboardData é o conjunto de dados que o painel carrega em uma requisição.
type DashboardData struct {
	Tenants    []domain.Tenant   `json:"tenants"`
	Properties []domain.Property `json:"properties"`
	Leases     []domain.Lease    `json:"leases"`
	Receipts   []domain.Receipt  `json:"receipts"`
	Metrics    MetricsPayload    `json:"metrics"`
}

// ListTenants devolve todos os locatários.
func ListTenants(ctx context.Context, deps Deps) ([]domain.Tenant, error) {
	tenants, err := deps.Repos.Tenants.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listar locatários: %w", err)
	}
	return tenants, nil
}

// ListProperties devolve todos os imóveis.
func ListProperties(ctx context.Context, deps Deps) ([]domain.Property, error) {
	properties, err := deps.Repos.Properties.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listar imóveis: %w", err)
	}
	return properties, nil
}

// ListLeases devolve todos os contratos, com nomes de locatário e imóvel.
func ListLeases(ctx context.Context, deps Deps) ([]domain.Lease, error) {
	leases, err := deps.Repos.Leases.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listar contratos: %w", err)
	}
	return leases, nil
}

// ListReceipts devolve todos os lançamentos, com nomes de locatário e imóvel.
func ListReceipts(ctx context.Context, deps Deps) ([]domain.Receipt, error) {
	receipts, err := deps.Repos.Receipts.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listar pagamentos: %w", err)
	}
	return receipts, nil
}

// DashboardSnapshot carrega tudo que o painel precisa em uma passada.
func DashboardSnapshot(ctx context.Context, deps Deps) (*DashboardData, error) {
	tenants, err := ListTenants(ctx, deps)
	if err != nil {
		return nil, err
	}
	properties, err := ListProperties(ctx, deps)
	if err != nil {
		return nil, err
	}
	leases, err := ListLeases(ctx, deps)
	if err != nil {
		return nil, err
	}
	receipts, err := ListReceipts(ctx, deps)
	if err != nil {
		return nil, err
	}
	metrics, err := ComputeMetrics(ctx, deps)
	if err != nil {
		return nil, err
	}
	return &DashboardData{
		Tenants:    tenants,
		Properties: properties,
		Leases:     leases,
		Receipts:   receipts,
		Metrics:    *metrics,
	}, nil
}
