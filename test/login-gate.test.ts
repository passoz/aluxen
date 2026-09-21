import { afterEach, describe, expect, it } from 'bun:test';
import { checkLoginAttempt, registerLoginFailure, registerLoginSuccess, resetLoginGate } from '../src/index';

afterEach(() => resetLoginGate());

describe('Proteção de login (rate limit e bloqueio)', () => {
  it('libera as tentativas dentro do limite por IP e bloqueia as excedentes', () => {
    const ip = '198.51.100.10';
    for (let attempt = 1; attempt <= 5; attempt++) {
      expect(checkLoginAttempt(ip).allowed).toBe(true);
    }

    const blocked = checkLoginAttempt(ip);
    expect(blocked.allowed).toBe(false);
    if (blocked.allowed) throw new Error('esperado bloqueio por IP');
    expect(blocked.reason).toBe('ip');
    expect(blocked.retryAfterSeconds).toBeGreaterThan(0);

    // Outro IP continua liberado (o limite é por origem)
    expect(checkLoginAttempt('198.51.100.11').allowed).toBe(true);
  });

  it('bloqueia o IP após falhas repetidas e libera quando a senha é acertada', () => {
    const ip = '198.51.100.20';
    for (let failure = 1; failure <= 10; failure++) {
      registerLoginFailure(ip);
    }

    const locked = checkLoginAttempt(ip);
    expect(locked.allowed).toBe(false);
    if (locked.allowed) throw new Error('esperado bloqueio por falhas');
    expect(locked.reason).toBe('locked');
    expect(locked.retryAfterSeconds).toBeGreaterThan(60);

    registerLoginSuccess(ip);
    expect(checkLoginAttempt(ip).allowed).toBe(true);
  });

  it('aplica o teto global de tentativas por minuto, mesmo com IPs diferentes', () => {
    for (let index = 0; index < 60; index++) {
      expect(checkLoginAttempt(`203.0.113.${index}`).allowed).toBe(true);
    }

    const blocked = checkLoginAttempt('203.0.113.200');
    expect(blocked.allowed).toBe(false);
    if (blocked.allowed) throw new Error('esperado bloqueio global');
    expect(blocked.reason).toBe('global');
  });
});
