import { describe, it, expect, beforeEach } from 'vitest';
import { loadConfig } from '../../lib/config';

describe('Config', () => {
  beforeEach(() => {
    delete process.env.DATABASE_URL;
    delete process.env.PORT;
  });

  it('should load default config', () => {
    const config = loadConfig();
    expect(config.databaseUrl).toBe('postgres://user:password@localhost:5432/rental?sslmode=disable');
    expect(config.port).toBe('3000');
    expect(config.secretKey).toBe('your-secret-key');
  });

  it('should load config from environment', () => {
    process.env.DATABASE_URL = 'postgres://test:test@localhost:5432/test';
    process.env.PORT = '4000';
    process.env.SECRET_KEY = 'test-secret';

    const config = loadConfig();
    expect(config.databaseUrl).toBe('postgres://test:test@localhost:5432/test');
    expect(config.port).toBe('4000');
    expect(config.secretKey).toBe('test-secret');
  });
});