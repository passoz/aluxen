-- Aluxen — schema canônico Postgres (fonte de verdade do sqlc para o dialeto Postgres).
--
-- Paridade obrigatória com schema/sqlite.sql: mesmas tabelas, colunas, nulidade,
-- chaves primárias, chaves estrangeiras (com a mesma ação de delete) e índices.
-- O mapa de equivalência de tipos é:
--   text    → text
--   integer → integer
--   real    → double precision
-- Divergência quebra o build (internal/infrastructure/db/parity).

CREATE TABLE users (
  id text PRIMARY KEY NOT NULL,
  username text NOT NULL,
  password_hash text NOT NULL,
  password_changed_at text,
  created_at text NOT NULL
);

CREATE UNIQUE INDEX users_username_unique ON users (username);

CREATE TABLE tenants (
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

CREATE TABLE properties (
  id text PRIMARY KEY NOT NULL,
  name text NOT NULL,
  address text NOT NULL,
  city text NOT NULL,
  state text NOT NULL,
  zip text,
  created_at text NOT NULL,
  updated_at text NOT NULL
);

CREATE TABLE leases (
  id text PRIMARY KEY NOT NULL,
  tenant_id text NOT NULL,
  property_id text NOT NULL,
  start_date text NOT NULL,
  end_date text NOT NULL,
  due_day integer NOT NULL,
  rent_amount double precision NOT NULL,
  status text NOT NULL,
  auth_token text,
  created_at text NOT NULL,
  updated_at text NOT NULL,
  FOREIGN KEY (tenant_id) REFERENCES tenants (id) ON UPDATE NO ACTION ON DELETE CASCADE,
  FOREIGN KEY (property_id) REFERENCES properties (id) ON UPDATE NO ACTION ON DELETE CASCADE
);

CREATE TABLE receipts (
  id text PRIMARY KEY NOT NULL,
  lease_id text,
  tenant_id text,
  property_id text,
  category text NOT NULL,
  description text,
  due_date text NOT NULL,
  paid_at text,
  amount double precision NOT NULL,
  paid_amount double precision,
  pdf_path text,
  generated_key text,
  auth_token text,
  created_at text NOT NULL,
  updated_at text,
  FOREIGN KEY (lease_id) REFERENCES leases (id) ON UPDATE NO ACTION ON DELETE CASCADE,
  FOREIGN KEY (tenant_id) REFERENCES tenants (id) ON UPDATE NO ACTION ON DELETE SET NULL,
  FOREIGN KEY (property_id) REFERENCES properties (id) ON UPDATE NO ACTION ON DELETE SET NULL
);

CREATE UNIQUE INDEX receipts_generated_key_idx ON receipts (generated_key);

CREATE TABLE settings (
  key text PRIMARY KEY NOT NULL,
  value text NOT NULL
);

CREATE TABLE audit_logs (
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

CREATE INDEX audit_logs_created_at_idx ON audit_logs (created_at);
CREATE INDEX audit_logs_entity_idx ON audit_logs (entity, entity_id);
CREATE INDEX audit_logs_action_idx ON audit_logs (action);
