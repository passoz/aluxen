-- +goose Up
-- Fila de trabalho assíncrono e os dois registros de leitura assistida por IA:
-- comprovantes de pagamento (baixa automática ou revisão humana) e cadastros
-- propostos a partir de documento. O resultado de 00001..00004 é idêntico a
-- schema/postgres.sql (verificado pelo teste de paridade).

CREATE TABLE IF NOT EXISTS jobs (
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

CREATE INDEX IF NOT EXISTS jobs_status_idx ON jobs (status, scheduled_at);
CREATE INDEX IF NOT EXISTS jobs_kind_idx ON jobs (kind, created_at);

CREATE TABLE IF NOT EXISTS payment_proofs (
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

CREATE INDEX IF NOT EXISTS payment_proofs_status_idx ON payment_proofs (status, created_at);
CREATE INDEX IF NOT EXISTS payment_proofs_tenant_idx ON payment_proofs (tenant_id);

CREATE TABLE IF NOT EXISTS document_intakes (
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

CREATE INDEX IF NOT EXISTS document_intakes_status_idx ON document_intakes (status, created_at);

-- +goose Down
DROP TABLE IF EXISTS document_intakes;
DROP TABLE IF EXISTS payment_proofs;
DROP TABLE IF EXISTS jobs;
