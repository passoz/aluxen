package whatsapp

import (
	"sync"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Registro em memória do andamento dos envios, como no app atual: o disparo é
// assíncrono (o WAHA leva alguns segundos para confirmar), então a API responde
// com um `jobId` e o painel acompanha as etapas por polling.
//
// O job pertence a quem o criou: o `jobId` de outro administrador não é
// consultável. Os tipos do job são os do caso de uso (application) porque o
// acompanhamento é o mesmo payload que o painel lê em
// /api/whatsapp/send/status.

// JobTTL é o tempo de vida de um job, igual ao `JOB_TTL_MS` do app atual.
const JobTTL = 5 * time.Minute

// JobStore guarda os jobs em memória e descarta os vencidos.
type JobStore struct {
	clock domain.Clock
	ids   domain.IDGenerator
	ttl   time.Duration

	mu   sync.Mutex
	jobs map[string]*application.WhatsAppJob
}

// NewJobStore cria a fila de acompanhamento dos envios.
func NewJobStore(clock domain.Clock, ids domain.IDGenerator) *JobStore {
	return &JobStore{clock: clock, ids: ids, ttl: JobTTL, jobs: map[string]*application.WhatsAppJob{}}
}

// Create abre um job para o administrador informado.
func (s *JobStore) Create(userID string) application.WhatsAppJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.prune(now)

	job := &application.WhatsAppJob{
		ID:        s.ids.New(),
		UserID:    userID,
		Status:    application.JobRunning,
		Stage:     application.StagePreparing,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.jobs[job.ID] = job
	return *job
}

// Stage publica a etapa atual do envio.
func (s *JobStore) Stage(jobID string, stage application.WhatsAppStage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return
	}
	job.Stage = stage
	job.UpdatedAt = s.now()
}

// Complete encerra o job com o resultado do envio.
func (s *JobStore) Complete(jobID string, result application.WhatsAppSendResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return
	}
	job.Status = application.JobDone
	job.Stage = application.StageDone
	job.Result = &result
	job.Error = ""
	job.UpdatedAt = s.now()
}

// Fail encerra o job com a razão da falha.
func (s *JobStore) Fail(jobID, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return
	}
	job.Status = application.JobError
	job.Error = reason
	job.UpdatedAt = s.now()
}

// Get devolve o job do administrador informado. O job de outro dono é
// indistinguível de um job inexistente.
func (s *JobStore) Get(jobID, userID string) (application.WhatsAppJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.prune(s.now())
	job, ok := s.jobs[jobID]
	if !ok || job.UserID != userID {
		return application.WhatsAppJob{}, false
	}
	return *job, true
}

// prune descarta os jobs parados há mais de JobTTL. Exige o mutex preso.
func (s *JobStore) prune(now int64) {
	for id, job := range s.jobs {
		if now-job.UpdatedAt > s.ttl.Milliseconds() {
			delete(s.jobs, id)
		}
	}
}

func (s *JobStore) now() int64 { return s.clock.Now().UnixMilli() }
