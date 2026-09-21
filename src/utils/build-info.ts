import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import path from 'node:path';

/**
 * Identificação do build em execução. Na imagem publicada os valores chegam por
 * variáveis de ambiente gravadas pelo workflow de publicação; em execução local
 * caem para o repositório git, quando disponível.
 *
 * A versão exibida é `MAJOR.MINOR` do `package.json` com o **número do build**
 * como patch: cada build incrementa a subversão (ex.: `1.0.42`), de modo que a
 * versão identifica exatamente o que está rodando.
 */
export type BuildInfo = {
  /** Versão efetiva do build: `MAJOR.MINOR.<número do build>`. */
  version: string;
  /** Versão declarada no `package.json` (base `MAJOR.MINOR`). */
  packageVersion: string;
  /** Número sequencial do build (commits da branch ou número da execução no CI). */
  buildNumber: number | null;
  /** Commit completo (40 caracteres) quando identificado. */
  commit: string | null;
  /** Commit curto — o mesmo sufixo usado na tag `sha-<commit>` da imagem. */
  shortCommit: string | null;
  /** Branch ou tag que originou o build. */
  ref: string | null;
  /** Momento em que a imagem foi construída (ISO 8601, UTC). */
  buildTime: string | null;
  /** Referência exata da imagem no GHCR, quando publicada pelo workflow. */
  image: string | null;
  /** Link da execução do GitHub Actions que gerou a imagem. */
  runUrl: string | null;
  /**
   * `image` quando o build veio do pipeline, `local` em execução de
   * desenvolvimento com repositório git e `unknown` quando não há metadados
   * (imagem publicada antes da injeção das variáveis de build).
   */
  source: 'image' | 'local' | 'unknown';
};

/** Variáveis gravadas na imagem pelo workflow de publicação e lidas em runtime. */
export const BUILD_ENV_VARS = [
  'ALUXEN_BUILD_COMMIT',
  'ALUXEN_BUILD_COMMIT_SHORT',
  'ALUXEN_BUILD_REF',
  'ALUXEN_BUILD_TIME',
  'ALUXEN_BUILD_IMAGE',
  'ALUXEN_BUILD_RUN_URL',
  'ALUXEN_BUILD_NUMBER',
] as const;

/** Executa um comando git de leitura e devolve a saída, ou `null` se indisponível. */
function readGitValue(args: string[]): string | null {
  try {
    const output = execFileSync('git', args, {
      cwd: process.cwd(),
      encoding: 'utf-8',
      stdio: ['ignore', 'pipe', 'ignore'],
    }).trim();
    return output && output !== 'HEAD' ? output : null;
  } catch {
    return null;
  }
}

const PACKAGE_VERSION = (() => {
  try {
    const raw = readFileSync(path.join(process.cwd(), 'package.json'), 'utf-8');
    return (JSON.parse(raw) as { version?: string }).version || '0.0.0';
  } catch {
    return '0.0.0';
  }
})();

const [ENV_COMMIT, ENV_COMMIT_SHORT, ENV_REF, ENV_TIME, ENV_IMAGE, ENV_RUN_URL, ENV_BUILD_NUMBER] =
  BUILD_ENV_VARS;
const readEnv = (name: string) => process.env[name]?.trim() || null;

const IMAGE_COMMIT = readEnv(ENV_COMMIT);
const GIT_COMMIT = readGitValue(['rev-parse', 'HEAD']);

/**
 * Número do build: informado pelo pipeline (`ALUXEN_BUILD_NUMBER`) ou, em
 * desenvolvimento, a quantidade de commits da branch corrente — sempre crescente
 * a cada build.
 */
const BUILD_NUMBER = (() => {
  const fromEnv = Number(readEnv(ENV_BUILD_NUMBER));
  if (Number.isInteger(fromEnv) && fromEnv > 0) return fromEnv;
  const commits = Number(readGitValue(['rev-list', '--count', 'HEAD']));
  return Number.isInteger(commits) && commits > 0 ? commits : null;
})();

const [major = '1', minor = '0'] = PACKAGE_VERSION.split('.');
const EFFECTIVE_VERSION = BUILD_NUMBER ? `${major}.${minor}.${BUILD_NUMBER}` : PACKAGE_VERSION;

const BUILD_INFO: BuildInfo = {
  version: EFFECTIVE_VERSION,
  packageVersion: PACKAGE_VERSION,
  buildNumber: BUILD_NUMBER,
  commit: IMAGE_COMMIT || GIT_COMMIT,
  shortCommit: readEnv(ENV_COMMIT_SHORT) || readGitValue(['rev-parse', '--short', 'HEAD']),
  ref: readEnv(ENV_REF) || readGitValue(['rev-parse', '--abbrev-ref', 'HEAD']),
  buildTime: readEnv(ENV_TIME),
  image: readEnv(ENV_IMAGE),
  runUrl: readEnv(ENV_RUN_URL),
  source: IMAGE_COMMIT ? 'image' : GIT_COMMIT ? 'local' : 'unknown',
};

export function getBuildInfo(): BuildInfo {
  return BUILD_INFO;
}
