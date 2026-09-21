-- +goose Up
-- Canal bidirecional do WhatsApp (WAHA). Tabela nova, criada depois do schema
-- consolidado de 00001 — o resultado de 00001 + 00002 é idêntico a
-- schema/sqlite.sql (verificado pelo teste de paridade).
--
-- `external_id` é o id da mensagem no WhatsApp; o índice único torna a
-- reentrega do webhook idempotente (o insert usa ON CONFLICT DO NOTHING).

CREATE TABLE IF NOT EXISTS whatsapp_messages (
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

CREATE UNIQUE INDEX IF NOT EXISTS whatsapp_messages_external_id_idx ON whatsapp_messages (external_id);
CREATE INDEX IF NOT EXISTS whatsapp_messages_tenant_idx ON whatsapp_messages (tenant_id, created_at);
CREATE INDEX IF NOT EXISTS whatsapp_messages_created_at_idx ON whatsapp_messages (created_at);

-- +goose Down
DROP TABLE IF EXISTS whatsapp_messages;
