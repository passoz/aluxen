package postgres

import (
	"context"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db/postgres/sqlcgen"
)

// Jobs implementa domain.JobRepository sobre o SQL gerado pelo sqlc: é a fila de
// trabalho assíncrono (leitura de documento, envio, cobrança).
type Jobs struct {
	queries *sqlcgen.Queries
}

// NewJobs monta o repositório da fila de trabalho.
func NewJobs(db sqlcgen.DBTX) *Jobs {
	return &Jobs{queries: sqlcgen.New(db)}
}

var _ domain.JobRepository = (*Jobs)(nil)

func (r *Jobs) Create(ctx context.Context, job *domain.Job) error {
	return wrapError(r.queries.InsertJob(ctx, sqlcgen.InsertJobParams{
		ID:          job.ID,
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
		Kind:        string(job.Kind),
		Status:      string(job.Status),
		Attempts:    int32(job.Attempts),
		MaxAttempts: int32(job.MaxAttempts),
		Payload:     nullString(job.Payload),
		Result:      nullString(job.Result),
		Error:       nullString(job.Error),
		ScheduledAt: job.ScheduledAt,
		StartedAt:   nullString(job.StartedAt),
		FinishedAt:  nullString(job.FinishedAt),
	}))
}

func (r *Jobs) ByID(ctx context.Context, id string) (*domain.Job, error) {
	row, err := r.queries.GetJobByID(ctx, id)
	if err != nil {
		return nil, wrapError(err)
	}
	job := jobFromRow(row)
	return &job, nil
}

func (r *Jobs) ListByStatus(ctx context.Context, status domain.JobStatus, limit int) ([]domain.Job, error) {
	rows, err := r.queries.ListJobsByStatus(ctx, sqlcgen.ListJobsByStatusParams{
		Status:    string(status),
		PageLimit: int32(limit),
	})
	if err != nil {
		return nil, wrapError(err)
	}
	return jobsFromRows(rows), nil
}

func (r *Jobs) List(ctx context.Context, limit int) ([]domain.Job, error) {
	rows, err := r.queries.ListJobs(ctx, int32(limit))
	if err != nil {
		return nil, wrapError(err)
	}
	return jobsFromRows(rows), nil
}

// Update grava o andamento do trabalho (estado, tentativas, resultado e erro).
func (r *Jobs) Update(ctx context.Context, job *domain.Job) error {
	return wrapError(r.queries.UpdateJob(ctx, sqlcgen.UpdateJobParams{
		UpdatedAt:   job.UpdatedAt,
		Status:      string(job.Status),
		Attempts:    int32(job.Attempts),
		Result:      nullString(job.Result),
		Error:       nullString(job.Error),
		ScheduledAt: job.ScheduledAt,
		StartedAt:   nullString(job.StartedAt),
		FinishedAt:  nullString(job.FinishedAt),
		ID:          job.ID,
	}))
}

func jobsFromRows(rows []sqlcgen.Job) []domain.Job {
	jobs := make([]domain.Job, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, jobFromRow(row))
	}
	return jobs
}

// jobFromRow converte a linha do banco na entidade de domínio.
func jobFromRow(row sqlcgen.Job) domain.Job {
	return domain.Job{
		ID:          row.ID,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		Kind:        domain.JobKind(row.Kind),
		Status:      domain.JobStatus(row.Status),
		Attempts:    int(row.Attempts),
		MaxAttempts: int(row.MaxAttempts),
		Payload:     stringPtr(row.Payload),
		Result:      stringPtr(row.Result),
		Error:       stringPtr(row.Error),
		ScheduledAt: row.ScheduledAt,
		StartedAt:   stringPtr(row.StartedAt),
		FinishedAt:  stringPtr(row.FinishedAt),
	}
}
