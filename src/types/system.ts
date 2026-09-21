import type { BuildInfo } from '../utils/build-info';

/** Payload de `GET /api/info`: identificação do build e estado geral do sistema. */
export type SystemInfo = {
  app: string;
  /** Versão efetiva exibida: `MAJOR.MINOR.<número do build>`. */
  version: string;
  /** Versão declarada no `package.json` (base `MAJOR.MINOR`). */
  packageVersion: string;
  /** Número sequencial do build que originou esta execução. */
  buildNumber: number | null;
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
