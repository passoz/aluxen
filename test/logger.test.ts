import { afterEach, describe, expect, it } from 'bun:test';
import { configureLogger, createLogger, getLoggerOptions } from '../src/utils/logger';

const originalOptions = getLoggerOptions();

/** Captura as linhas emitidas pelo logger durante `run()` e devolve os JSONs parseados. */
function captureJsonLines(run: () => void): Array<Record<string, unknown>> {
  const stdout = process.stdout;
  const originalWrite = stdout.write;
  const lines: string[] = [];
  stdout.write = ((chunk: unknown) => {
    lines.push(String(chunk));
    return true;
  }) as unknown as typeof stdout.write;
  try {
    configureLogger({ format: 'json' });
    run();
  } finally {
    stdout.write = originalWrite;
  }
  return lines
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => JSON.parse(line) as Record<string, unknown>);
}

afterEach(() => {
  configureLogger(originalOptions);
});

describe('Logger estruturado', () => {
  it('redige campos sensíveis preservando os demais', () => {
    const lines = captureJsonLines(() => {
      createLogger('teste').info('Login do usuário', { user: 'passoz', password: 'segredo-123', sessionToken: 'abc999' });
    });

    expect(lines).toHaveLength(1);
    const entry = lines[0]!;
    expect(entry['level']).toBe('info');
    expect(entry['scope']).toBe('teste');
    expect(entry['user']).toBe('passoz');
    expect(entry['password']).toBe('[redigido]');
    expect(entry['sessionToken']).toBe('[redigido]');
    expect(JSON.stringify(entry)).not.toContain('segredo-123');
  });

  it('respeita o nível configurado', () => {
    const lines = captureJsonLines(() => {
      configureLogger({ level: 'warn' });
      const log = createLogger('teste');
      log.debug('detalhe irrelevante');
      log.info('informação');
      log.warn('atenção');
    });

    expect(lines.map((line) => line['level'])).toEqual(['warn']);
    expect(lines[0]!['message']).toBe('atenção');
  });

  it('serializa erros com mensagem e rastreio de pilha', () => {
    const lines = captureJsonLines(() => {
      createLogger('teste').error('Falha ao processar', { error: new Error('quebrou') });
    });

    expect(lines).toHaveLength(1);
    const error = lines[0]!['error'] as Record<string, unknown>;
    expect(error['message']).toBe('quebrou');
    expect(typeof error['stack']).toBe('string');
  });

  it('emite linha legível com timestamp, nível e escopo no formato pretty', () => {
    const stdout = process.stdout;
    const originalWrite = stdout.write;
    let output = '';
    stdout.write = ((chunk: unknown) => {
      output += String(chunk);
      return true;
    }) as unknown as typeof stdout.write;
    try {
      configureLogger({ format: 'pretty', level: 'debug' });
      createLogger('http').warn('Requisição recusada', { status: 404 });
    } finally {
      stdout.write = originalWrite;
    }

    expect(output).toMatch(/^\d{4}-\d{2}-\d{2}T[\d:.]+Z WARN {2}\[http\] Requisição recusada status=404\n$/);
  });
});
