package whatsapp

import (
	"sync"
	"time"

	"github.com/passoz/aluxen/internal/domain"
)

// Registro em memória do andamento dos envios, como no app atual: o disparo é
// assíncrono (o WAHA leva alguns segundos para confirmar), então a API responde
// com um `jobId` e o painel acompanha as etapas por polling.
//
// O job pertence a quem o criou: o `jobId` de outro administrador não é
// consultável.

// JobTTL é o tempo de vida de um job, igual ao `JOB_TTL_MS` do app atual.
const JobTTL = 5 * time.Minute

// JobStatus é o desfecho do envio.
type JobStatus string

const (
	JobRunning JobStatus = "running"
	JobDone    JobStatus = "done"
	JobError   JobStatus = "error"
)

// SendStage é a etapa observável do envio, sem detalhes técnicos.
type SendStage string

const (
	StagePreparing   SendStage = "preparing"
	StageConnecting  SendStage = "connecting"
	StageOpeningChat SendStage = "opening_chat"
	StageSending     SendStage = "sending"
	StageConfirming  SendStage = "confirming"
	StageDone        SendStage = "done"
)

// SendResult é o resultado do envio concluído.
type SendResult struct {
	Success   bool   `json:"success"`
	To        string `json:"to"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Error     string `json:"error,omitempty"`
}

// Job é o acompanhamento de um envio.
type Job struct {
	ID        string      `json:"id"`
	UserID    string      `json:"userId"`
	Status    JobStatus   `json:"status"`
	Stage     SendStage   `json:"stage"`
	Result    *SendResult `json:"result,omitempty"`
	Error     string      `json:"error,omitempty"`
	CreatedAt int64       `json:"createdAt"`
	UpdatedAt int64       `json:"updatedAt"`
}

// JobStore guarda os jobs em memória e descarta os vencidos.
type JobStore struct {
	clock domain.Clock
	ids   domain.IDGenerator
	ttl   time.Duration

	mu   sync.Mutex
	jobs map[string]*Job
}

// NewJobStore cria a fila de acompanhamento dos envios.
func NewJobStore(clock domain.Clock, ids domain.IDGenerator) *JobStore {
	return &JobStore{clock: clock, ids: ids, ttl: JobTTL, jobs: map[string]*Job{}}
}

// Create abre um job para o administrador informado.
func (s *JobStore) Create(userID string) Job {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.prune(now)

	job := &Job{
		ID:        s.ids.New(),
		UserID:    userID,
		Status:    JobRunning,
		Stage:     StagePreparing,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.jobs[job.ID] = job
	return *job
}

// Stage publica a etapa atual do envio.
func (s *JobStore) Stage(id string, stage SendStage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok {
		return
	}
	job.Stage = stage
	job.UpdatedAt = s.now()
}

// Complete encerra o job com o resultado do envio.
func (s *JobStore) Complete(id string, result SendResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok {
		return
	}
	job.Status = JobDone
	job.Stage = StageDone
	job.Result = &result
	job.Error = ""
	job.UpdatedAt = s.now()
}

// Fail encerra o job com a razão da falha.
func (s *JobStore) Fail(id, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok {
		return
	}
	job.Status = JobError
	job.Error = reason
	job.UpdatedAt = s.now()
}

// Get devolve o job do administrador informado. O job de outro dono é
// indistinguível de um job inexistente.
func (s *JobStore) Get(id, userID string) (Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.prune(s.now())
	job, ok := s.jobs[id]
	if !ok || job.UserID != userID {
		return Job{}, false
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
