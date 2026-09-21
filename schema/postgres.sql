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
  deposit_amount double precision,
  deposit_status text,
  deposit_received_at text,
  deposit_refunded_at text,
  deposit_refunded_amount double precision,
  deposit_notes text,
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

-- Canal bidirecional do WhatsApp (WAHA): cada mensagem recebida pelo webhook
-- entra vinculada ao locatário pelo telefone (`tenant_id` nulo enquanto não
-- houver titular com aquele número). `external_id` é o id da mensagem no
-- WhatsApp e é único, o que torna a reentrega do webhook idempotente.
CREATE TABLE whatsapp_messages (
  id text PRIMARY KEY NOT NULL,
  created_at text NOT NULL,
  direction text NOT NULL,
  chat_id text NOT NULL,
  phone text NOT NULL,
  tenant_id text,
  body text NOT NULL,
  status text NOT NULL,
  external_id text,
  metadata text,
  FOREIGN KEY (tenant_id) REFERENCES tenants (id) ON UPDATE NO ACTION ON DELETE SET NULL
);

CREATE UNIQUE INDEX whatsapp_messages_external_id_idx ON whatsapp_messages (external_id);
CREATE INDEX whatsapp_messages_tenant_idx ON whatsapp_messages (tenant_id, created_at);
CREATE INDEX whatsapp_messages_created_at_idx ON whatsapp_messages (created_at);

CREATE TABLE inspections (
  id text PRIMARY KEY NOT NULL,
  lease_id text NOT NULL,
  kind text NOT NULL,
  performed_at text NOT NULL,
  condition text NOT NULL,
  notes text,
  items text,
  photos text,
  auth_token text,
  created_at text NOT NULL,
  updated_at text NOT NULL,
  FOREIGN KEY (lease_id) REFERENCES leases (id) ON UPDATE NO ACTION ON DELETE CASCADE
);

CREATE INDEX inspections_lease_idx ON inspections (lease_id);
CREATE INDEX inspections_performed_at_idx ON inspections (performed_at);

CREATE TABLE jobs (
  id text PRIMARY KEY NOT NULL,
  created_at text NOT NULL,
  updated_at text NOT NULL,
  kind text NOT NULL,
  status text NOT NULL,
  attempts integer NOT NULL,
  max_attempts integer NOT NULL,
  payload text,
  result text,
  error text,
  scheduled_at text NOT NULL,
  started_at text,
  finished_at text
);

CREATE INDEX jobs_status_idx ON jobs (status, scheduled_at);
CREATE INDEX jobs_kind_idx ON jobs (kind, created_at);

CREATE TABLE payment_proofs (
  id text PRIMARY KEY NOT NULL,
  created_at text NOT NULL,
  updated_at text NOT NULL,
  source text NOT NULL,
  phone text,
  tenant_id text,
  receipt_id text,
  file_name text,
  file_path text,
  mime text,
  extracted_amount double precision,
  extracted_date text,
  extracted_payer text,
  extracted_payee text,
  extracted_identifier text,
  confidence double precision,
  status text NOT NULL,
  decision text NOT NULL,
  decision_reason text,
  decided_by text,
  decided_at text,
  raw_response text,
  FOREIGN KEY (tenant_id) REFERENCES tenants (id) ON UPDATE NO ACTION ON DELETE SET NULL,
  FOREIGN KEY (receipt_id) REFERENCES receipts (id) ON UPDATE NO ACTION ON DELETE SET NULL
);

CREATE INDEX payment_proofs_status_idx ON payment_proofs (status, created_at);
CREATE INDEX payment_proofs_tenant_idx ON payment_proofs (tenant_id);

CREATE TABLE document_intakes (
  id text PRIMARY KEY NOT NULL,
  created_at text NOT NULL,
  updated_at text NOT NULL,
  kind text NOT NULL,
  file_name text,
  file_path text,
  mime text,
  status text NOT NULL,
  extracted text,
  confidence double precision,
  created_entity text,
  created_entity_id text,
  raw_response text,
  notes text
);

CREATE INDEX document_intakes_status_idx ON document_intakes (status, created_at);
