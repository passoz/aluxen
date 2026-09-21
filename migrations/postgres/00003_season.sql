-- +goose Up
-- Temporada: caução no contrato e autos de vistoria. Tudo aditivo — o resultado
-- de 00001 + 00002 + 00003 é idêntico a schema/postgres.sql (verificado pelo
-- teste de paridade).
--
-- A caução vive no próprio contrato porque é dinheiro de UM contrato, com
-- entrada e devolução registradas nele. A vistoria ganhou tabela própria porque
-- são várias por contrato (entrada e saída) e cada uma carrega itens e fotos.

ALTER TABLE leases ADD COLUMN deposit_amount double precision;
ALTER TABLE leases ADD COLUMN deposit_status text;
ALTER TABLE leases ADD COLUMN deposit_received_at text;
ALTER TABLE leases ADD COLUMN deposit_refunded_at text;
ALTER TABLE leases ADD COLUMN deposit_refunded_amount double precision;
ALTER TABLE leases ADD COLUMN deposit_notes text;

CREATE TABLE IF NOT EXISTS inspections (
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

CREATE INDEX IF NOT EXISTS inspections_lease_idx ON inspections (lease_id);
CREATE INDEX IF NOT EXISTS inspections_performed_at_idx ON inspections (performed_at);

-- +goose Down
DROP TABLE IF EXISTS inspections;
ALTER TABLE leases DROP COLUMN deposit_notes;
ALTER TABLE leases DROP COLUMN deposit_refunded_amount;
ALTER TABLE leases DROP COLUMN deposit_refunded_at;
ALTER TABLE leases DROP COLUMN deposit_received_at;
ALTER TABLE leases DROP COLUMN deposit_status;
ALTER TABLE leases DROP COLUMN deposit_amount;
