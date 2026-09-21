package web

import (
	"context"

	"github.com/passoz/aluxen/internal/domain"
)

// emptyInspections é o dublê de vistoria usado pelos harnesses de teste que não
// exercitam temporada: o painel carrega a visão de temporada em toda
// requisição, então a porta precisa estar ligada.
type emptyInspections struct {
	items []domain.Inspection
}

func (r *emptyInspections) List(context.Context) ([]domain.Inspection, error) { return r.items, nil }

func (r *emptyInspections) ByID(_ context.Context, id string) (*domain.Inspection, error) {
	for index := range r.items {
		if r.items[index].ID == id {
			return &r.items[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *emptyInspections) ByLease(_ context.Context, leaseID string) ([]domain.Inspection, error) {
	filtered := make([]domain.Inspection, 0, len(r.items))
	for _, inspection := range r.items {
		if inspection.LeaseID == leaseID {
			filtered = append(filtered, inspection)
		}
	}
	return filtered, nil
}

func (r *emptyInspections) Create(_ context.Context, inspection *domain.Inspection) error {
	r.items = append(r.items, *inspection)
	return nil
}

func (r *emptyInspections) Update(_ context.Context, inspection *domain.Inspection) error {
	for index := range r.items {
		if r.items[index].ID == inspection.ID {
			r.items[index] = *inspection
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *emptyInspections) Delete(_ context.Context, id string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}
