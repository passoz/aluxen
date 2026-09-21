import type { BuildInfo } from '../utils/build-info';

/** Payload de `GET /api/info`: identificação do build e estado geral do sistema. */
export type SystemInfo = {
  app: string;
  version: string;
  runtime: string;
  nodeEnv: string;
  timezone: string;
  startedAt: string;
  uptimeSeconds: number;
  database: {
    path: string;
    sizeBytes: number;
  };
  counts: {
    tenants: number;
    properties: number;
    leases: number;
    receipts: number;
  };
  appUrl: string;
  build: BuildInfo;
};
