import type { SendJob, SendResult, SendStage } from './types';

/**
 * Registro em memória do andamento dos envios automáticos.
 *
 * O disparo é assíncrono (o navegador leva alguns segundos para abrir a conversa,
 * enviar e confirmar a transmissão), então a API responde imediatamente com um
 * `jobId` e o cliente acompanha as etapas por polling.
 */

const JOB_TTL_MS = 5 * 60 * 1000;

const jobs = new Map<string, SendJob>();

function prune(now = Date.now()) {
  for (const [id, job] of jobs) {
    if (now - job.updatedAt > JOB_TTL_MS) {
      jobs.delete(id);
    }
  }
}

export function createSendJob(userId: string): SendJob {
  prune();
  const now = Date.now();
  const job: SendJob = {
    id: crypto.randomUUID(),
    userId,
    status: 'running',
    stage: 'preparing',
    createdAt: now,
    updatedAt: now,
  };
  jobs.set(job.id, job);
  return job;
}

export function updateSendJobStage(id: string, stage: SendStage): void {
  const job = jobs.get(id);
  if (!job) return;
  job.stage = stage;
  job.updatedAt = Date.now();
}

export function completeSendJob(id: string, result: SendResult): void {
  const job = jobs.get(id);
  if (!job) return;
  job.status = 'done';
  job.stage = 'done';
  job.result = result;
  job.updatedAt = Date.now();
}

export function failSendJob(id: string, error: string): void {
  const job = jobs.get(id);
  if (!job) return;
  job.status = 'error';
  job.error = error;
  job.updatedAt = Date.now();
}

export function getSendJob(id: string): SendJob | undefined {
  prune();
  return jobs.get(id);
}
