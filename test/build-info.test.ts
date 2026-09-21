import { describe, expect, it } from 'bun:test';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { BUILD_ENV_VARS, getBuildInfo } from '../src/utils/build-info';

const dockerfile = readFileSync(path.join(process.cwd(), 'Dockerfile'), 'utf-8');
const workflow = readFileSync(path.join(process.cwd(), '.github/workflows/publish-image.yml'), 'utf-8');

describe('Identificação do build', () => {
  it('declara as variáveis de build no Dockerfile e as repassa no workflow de publicação', () => {
    for (const name of BUILD_ENV_VARS) {
      expect(dockerfile).toContain(`ARG ${name}=`);
      expect(dockerfile).toContain(`${name}=$${name}`);
      expect(workflow).toContain(`${name}=`);
    }
  });

  it('resolve versão e commit do repositório quando roda fora da imagem', () => {
    const info = getBuildInfo();
    expect(info.packageVersion).toMatch(/^\d+\.\d+\.\d+/);
    // A subversão exibida é o número do build (commits da branch), sempre crescente.
    expect(info.buildNumber).toBeGreaterThan(0);
    const [major, minor] = info.packageVersion.split('.');
    expect(info.version).toBe(`${major}.${minor}.${info.buildNumber}`);
    expect(info.shortCommit).toMatch(/^[0-9a-f]{7,}$/);
    expect(info.commit).toMatch(/^[0-9a-f]{40}$/);
    expect(info.source).toBe('local');
  });
});
