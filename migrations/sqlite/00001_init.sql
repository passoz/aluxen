-- +goose Up
-- Aluxen — schema final do dialeto SQLite.
--
-- Este arquivo é o resultado da consolidação das migrações Drizzle
-- (drizzle/0000..0005) e dos remendos de `src/db/index.ts`. Ele é IDEMPOTENTE:
-- tudo usa IF NOT EXISTS, então uma base viva (que já tem as tabelas) pode
-- receber a migração sem recriar nada. A adoção de bases legadas — colunas
-- faltantes e o rebuild de `receipts` — acontece em
-- internal/infrastructure/db/migrate.go, antes do goose.
--
-- O resultado deste arquivo num banco vazio é idêntico a schema/sqlite.sql
-- (verificado pelo teste de paridade).

CREATE TABLE IF NOT EXISTS users (
  id text PRIMARY KEY NOT NULL,
  username text NOT NULL,
  password_hash text NOT NULL,
  password_changed_at text,
  created_at text NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS users_username_unique ON users (username);

CREATE TABLE IF NOT EXISTS tenants (
  id text PRIMARY KEY NOT NULL,
  name text NOT NULL,
  email text NOT NULL,
  phone text,
  cpf text,
  password_hash text,
  password_changed_at text,
  created_at text NOT NULL,
  updated_at text NOT NULL
);

CREATE TABLE IF NOT EXISTS properties (
  id text PRIMARY KEY NOT NULL,
  name text NOT NULL,
  address text NOT NULL,
  city text NOT NULL,
  state text NOT NULL,
  zip text,
  created_at text NOT NULL,
  updated_at text NOT NULL
);

CREATE TABLE IF NOT EXISTS leases (
  id text PRIMARY KEY NOT NULL,
  tenant_id text NOT NULL,
  property_id text NOT NULL,
  start_date text NOT NULL,
  end_date text NOT NULL,
  due_day integer NOT NULL,
  rent_amount real NOT NULL,
  status text NOT NULL,
  auth_token text,
  created_at text NOT NULL,
  updated_at text NOT NULL,
  FOREIGN KEY (tenant_id) REFERENCES tenants (id) ON UPDATE NO ACTION ON DELETE CASCADE,
  FOREIGN KEY (property_id) REFERENCES properties (id) ON UPDATE NO ACTION ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS receipts (
  id text PRIMARY KEY NOT NULL,
  lease_id text,
  tenant_id text,
  property_id text,
  category text NOT NULL,
  description text,
  due_date text NOT NULL,
  paid_at text,
  amount real NOT NULL,
  paid_amount real,
  pdf_path text,
  generated_key text,
  auth_token text,
  created_at text NOT NULL,
  updated_at text,
  FOREIGN KEY (lease_id) REFERENCES leases (id) ON UPDATE NO ACTION ON DELETE CASCADE,
  FOREIGN KEY (tenant_id) REFERENCES tenants (id) ON UPDATE NO ACTION ON DELETE SET NULL,
  FOREIGN KEY (property_id) REFERENCES properties (id) ON UPDATE NO ACTION ON DELETE SET NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS receipts_generated_key_idx ON receipts (generated_key);

CREATE TABLE IF NOT EXISTS settings (
  key text PRIMARY KEY NOT NULL,
  value text NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id text PRIMARY KEY NOT NULL,
  created_at text NOT NULL,
  actor_id text,
  actor_name text,
  action text NOT NULL,
  entity text NOT NULL,
  entity_id text,
  entity_label text,
  summary text NOT NULL,
  changes text,
  metadata text,
  origin text NOT NULL,
  ip text,
  user_agent text,
  request_id text
);

CREATE INDEX IF NOT EXISTS audit_logs_created_at_idx ON audit_logs (created_at);
CREATE INDEX IF NOT EXISTS audit_logs_entity_idx ON audit_logs (entity, entity_id);
CREATE INDEX IF NOT EXISTS audit_logs_action_idx ON audit_logs (action);

-- +goose Down
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS receipts;
DROP TABLE IF EXISTS leases;
DROP TABLE IF EXISTS properties;
DROP TABLE IF EXISTS tenants;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS users;
