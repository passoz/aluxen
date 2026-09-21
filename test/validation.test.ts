import { describe, expect, it } from 'bun:test';
import { isValidEmail } from '../src/utils/validation';

describe('Validação de e-mail', () => {
  it('aceita endereços com domínio e TLD', () => {
    for (const value of ['nome@dominio.com', 'nome.sobrenome@dominio.com.br', 'contato+aluguel@teste.io']) {
      expect(isValidEmail(value)).toBe(true);
    }
  });

  it('recusa endereços sem arroba, sem domínio ou com espaços', () => {
    for (const value of ['sem-arroba.teste', 'nome@dominio', 'nome@.com', 'nome @dominio.com', 'nome@dominio.c', '']) {
      expect(isValidEmail(value)).toBe(false);
    }
  });
});
