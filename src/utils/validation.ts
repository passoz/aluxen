/**
 * Formato mínimo aceito para e-mail: `usuario@dominio.tld`, sem espaços e com
 * domínio de nível superior de pelo menos duas letras. Usado tanto na validação
 * nativa dos formulários (atributo `pattern`) quanto no servidor.
 */
export const EMAIL_PATTERN_SOURCE = '^[^\\s@]+@[^\\s@]+\\.[A-Za-z]{2,}$';

const EMAIL_PATTERN = new RegExp(EMAIL_PATTERN_SOURCE);

export function isValidEmail(value: string | null | undefined): boolean {
  return EMAIL_PATTERN.test((value || '').trim());
}
