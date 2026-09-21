-- Aluxen - queries compartilhadas pelos dois dialetos.
--
-- O MESMO arquivo alimenta o sqlc do SQLite e o do Postgres. Para isso:
--   * parametros usam sqlc.arg(nome), que cada dialeto traduz para a sua sintaxe;
--   * nada de funcoes de data do banco (o timestamp e escrito pelo codigo);
--   * comparacoes de texto usam lower() nos dois lados, porque LIKE e
--     case-insensitive no SQLite e case-sensitive no Postgres;
--   * listas de ids sao percorridas em transacao pela aplicacao (sem slice de IN),
--     para nao depender de extensao especifica de dialeto;
--   * este arquivo precisa ser ASCII puro: o parser do engine sqlite do sqlc
--     quebra com caracteres multi-byte.

-- -- users ----------------------------------------

-- name: GetUserByID :one
SELECT * FROM users WHERE id = sqlc.arg(id);

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = sqlc.arg(username);

-- name: InsertUser :exec
INSERT INTO users (id, username, password_hash, password_changed_at, created_at)
VALUES (sqlc.arg(id), sqlc.arg(username), sqlc.arg(password_hash), sqlc.arg(password_changed_at), sqlc.arg(created_at));

-- name: UpdateUserUsername :exec
UPDATE users SET username = sqlc.arg(username) WHERE id = sqlc.arg(id);

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = sqlc.arg(password_hash), password_changed_at = sqlc.arg(password_changed_at)
WHERE id = sqlc.arg(id);

-- -- tenants --------------------------------------

-- name: ListTenants :many
SELECT * FROM tenants ORDER BY name ASC;

-- name: GetTenantByID :one
SELECT * FROM tenants WHERE id = sqlc.arg(id);

-- name: GetTenantByCPF :one
SELECT * FROM tenants
WHERE replace(replace(replace(coalesce(cpf, ''), '.', ''), '-', ''), ' ', '') = sqlc.arg(cpf)
ORDER BY created_at ASC
LIMIT 1;

-- name: InsertTenant :exec
INSERT INTO tenants (id, name, email, phone, cpf, password_hash, password_changed_at, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(email), sqlc.arg(phone), sqlc.arg(cpf),
        sqlc.arg(password_hash), sqlc.arg(password_changed_at), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: UpdateTenant :exec
UPDATE tenants
SET name = sqlc.arg(name), email = sqlc.arg(email), phone = sqlc.arg(phone), cpf = sqlc.arg(cpf), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: UpdateTenantPassword :exec
UPDATE tenants
SET password_hash = sqlc.arg(password_hash), password_changed_at = sqlc.arg(password_changed_at), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: DeleteTenant :exec
DELETE FROM tenants WHERE id = sqlc.arg(id);

-- name: CountTenants :one
SELECT COUNT(*) AS total FROM tenants;


-- -- properties -------------------------------------

-- name: ListProperties :many
SELECT * FROM properties ORDER BY name ASC;

-- name: GetPropertyByID :one
SELECT * FROM properties WHERE id = sqlc.arg(id);

-- name: InsertProperty :exec
INSERT INTO properties (id, name, address, city, state, zip, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(address), sqlc.arg(city), sqlc.arg(state), sqlc.arg(zip),
        sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: UpdateProperty :exec
UPDATE properties
SET name = sqlc.arg(name), address = sqlc.arg(address), city = sqlc.arg(city), state = sqlc.arg(state),
    zip = sqlc.arg(zip), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: DeleteProperty :exec
DELETE FROM properties WHERE id = sqlc.arg(id);

-- name: CountProperties :one
SELECT COUNT(*) AS total FROM properties;


-- -- leases ---------------------------------------

-- name: ListLeases :many
SELECT l.*, t.name AS tenant_name, p.name AS property_name
FROM leases l
JOIN tenants t ON t.id = l.tenant_id
JOIN properties p ON p.id = l.property_id
ORDER BY l.start_date DESC, t.name ASC;

-- name: GetLeaseByID :one
SELECT l.*, t.name AS tenant_name, p.name AS property_name
FROM leases l
JOIN tenants t ON t.id = l.tenant_id
JOIN properties p ON p.id = l.property_id
WHERE l.id = sqlc.arg(id);

-- name: ListLeasesByTenant :many
SELECT l.*, t.name AS tenant_name, p.name AS property_name
FROM leases l
JOIN tenants t ON t.id = l.tenant_id
JOIN properties p ON p.id = l.property_id
WHERE l.tenant_id = sqlc.arg(tenant_id)
ORDER BY l.start_date DESC;

-- name: ListLeasesByProperty :many
SELECT l.*, t.name AS tenant_name, p.name AS property_name
FROM leases l
JOIN tenants t ON t.id = l.tenant_id
JOIN properties p ON p.id = l.property_id
WHERE l.property_id = sqlc.arg(property_id)
ORDER BY l.start_date DESC;

-- name: ListLeasesOverlapping :many
SELECT l.*, t.name AS tenant_name, p.name AS property_name
FROM leases l
JOIN tenants t ON t.id = l.tenant_id
JOIN properties p ON p.id = l.property_id
WHERE l.property_id = sqlc.arg(property_id)
  AND l.id <> sqlc.arg(exclude_id)
  AND l.status IN ('active', 'season')
  AND l.start_date <= sqlc.arg(end_date)
  AND l.end_date >= sqlc.arg(start_date);

-- name: ListLeasesActiveInRange :many
SELECT l.*, t.name AS tenant_name, p.name AS property_name
FROM leases l
JOIN tenants t ON t.id = l.tenant_id
JOIN properties p ON p.id = l.property_id
WHERE l.status = 'active'
  AND l.start_date <= sqlc.arg(end_date)
  AND l.end_date >= sqlc.arg(start_date)
ORDER BY l.start_date ASC;

-- name: InsertLease :exec
INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, due_day, rent_amount, status, auth_token,
                    deposit_amount, deposit_status, deposit_received_at, deposit_refunded_at,
                    deposit_refunded_amount, deposit_notes, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(tenant_id), sqlc.arg(property_id), sqlc.arg(start_date), sqlc.arg(end_date),
        sqlc.arg(due_day), sqlc.arg(rent_amount), sqlc.arg(status), sqlc.arg(auth_token),
        sqlc.arg(deposit_amount), sqlc.arg(deposit_status), sqlc.arg(deposit_received_at),
        sqlc.arg(deposit_refunded_at), sqlc.arg(deposit_refunded_amount), sqlc.arg(deposit_notes),
        sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: UpdateLease :exec
UPDATE leases
SET tenant_id = sqlc.arg(tenant_id), property_id = sqlc.arg(property_id), start_date = sqlc.arg(start_date),
    end_date = sqlc.arg(end_date), due_day = sqlc.arg(due_day), rent_amount = sqlc.arg(rent_amount),
    status = sqlc.arg(status),
    deposit_amount = sqlc.arg(deposit_amount), deposit_status = sqlc.arg(deposit_status),
    deposit_received_at = sqlc.arg(deposit_received_at), deposit_refunded_at = sqlc.arg(deposit_refunded_at),
    deposit_refunded_amount = sqlc.arg(deposit_refunded_amount), deposit_notes = sqlc.arg(deposit_notes),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: UpdateLeaseStatus :exec
UPDATE leases SET status = sqlc.arg(status), updated_at = sqlc.arg(updated_at) WHERE id = sqlc.arg(id);

-- name: DeleteLease :exec
DELETE FROM leases WHERE id = sqlc.arg(id);

-- name: CountLeases :one
SELECT COUNT(*) AS total FROM leases;

-- name: CountLeasesByStatus :one
SELECT COUNT(*) AS total FROM leases WHERE status = sqlc.arg(status);


-- -- receipts -------------------------------------

-- name: ListReceipts :many
SELECT r.*,
       coalesce(t.name, '') AS tenant_name,
       coalesce(p.name, '') AS property_name
FROM receipts r
LEFT JOIN leases l ON l.id = r.lease_id
LEFT JOIN tenants t ON t.id = coalesce(l.tenant_id, r.tenant_id)
LEFT JOIN properties p ON p.id = coalesce(l.property_id, r.property_id)
ORDER BY r.due_date DESC, r.created_at DESC;

-- name: GetReceiptByID :one
SELECT r.*,
       coalesce(t.name, '') AS tenant_name,
       coalesce(p.name, '') AS property_name
FROM receipts r
LEFT JOIN leases l ON l.id = r.lease_id
LEFT JOIN tenants t ON t.id = coalesce(l.tenant_id, r.tenant_id)
LEFT JOIN properties p ON p.id = coalesce(l.property_id, r.property_id)
WHERE r.id = sqlc.arg(id);

-- name: ListReceiptsByTenant :many
SELECT r.*,
       coalesce(t.name, '') AS tenant_name,
       coalesce(p.name, '') AS property_name
FROM receipts r
LEFT JOIN leases l ON l.id = r.lease_id
LEFT JOIN tenants t ON t.id = coalesce(l.tenant_id, r.tenant_id)
LEFT JOIN properties p ON p.id = coalesce(l.property_id, r.property_id)
WHERE r.tenant_id = sqlc.arg(tenant_id) OR l.tenant_id = sqlc.arg(tenant_id)
ORDER BY r.due_date DESC;

-- name: ListReceiptsByLease :many
SELECT r.*,
       coalesce(t.name, '') AS tenant_name,
       coalesce(p.name, '') AS property_name
FROM receipts r
LEFT JOIN leases l ON l.id = r.lease_id
LEFT JOIN tenants t ON t.id = coalesce(l.tenant_id, r.tenant_id)
LEFT JOIN properties p ON p.id = coalesce(l.property_id, r.property_id)
WHERE r.lease_id = sqlc.arg(lease_id)
ORDER BY r.due_date DESC;

-- name: InsertReceipt :exec
INSERT INTO receipts (id, lease_id, tenant_id, property_id, category, description, due_date, paid_at, amount,
                      paid_amount, pdf_path, generated_key, auth_token, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(lease_id), sqlc.arg(tenant_id), sqlc.arg(property_id), sqlc.arg(category),
        sqlc.arg(description), sqlc.arg(due_date), sqlc.arg(paid_at), sqlc.arg(amount), sqlc.arg(paid_amount),
        sqlc.arg(pdf_path), sqlc.arg(generated_key), sqlc.arg(auth_token), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: InsertReceiptIfAbsent :execrows
INSERT INTO receipts (id, lease_id, tenant_id, property_id, category, description, due_date, paid_at, amount,
                      paid_amount, pdf_path, generated_key, auth_token, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(lease_id), sqlc.arg(tenant_id), sqlc.arg(property_id), sqlc.arg(category),
        sqlc.arg(description), sqlc.arg(due_date), sqlc.arg(paid_at), sqlc.arg(amount), sqlc.arg(paid_amount),
        sqlc.arg(pdf_path), sqlc.arg(generated_key), sqlc.arg(auth_token), sqlc.arg(created_at), sqlc.arg(updated_at))
ON CONFLICT (generated_key) DO NOTHING;

-- name: UpdateReceipt :exec
UPDATE receipts
SET lease_id = sqlc.arg(lease_id), tenant_id = sqlc.arg(tenant_id), property_id = sqlc.arg(property_id),
    category = sqlc.arg(category), description = sqlc.arg(description), due_date = sqlc.arg(due_date),
    paid_at = sqlc.arg(paid_at), amount = sqlc.arg(amount), paid_amount = sqlc.arg(paid_amount),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: UpdateReceiptPayment :exec
UPDATE receipts
SET paid_at = sqlc.arg(paid_at), paid_amount = sqlc.arg(paid_amount), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: DeleteReceipt :exec
DELETE FROM receipts WHERE id = sqlc.arg(id);

-- name: CountReceipts :one
SELECT COUNT(*) AS total FROM receipts;


-- -- settings -------------------------------------

-- name: GetSetting :one
SELECT value FROM settings WHERE key = sqlc.arg(key);

-- name: ListSettings :many
SELECT * FROM settings ORDER BY key ASC;

-- name: UpsertSetting :exec
INSERT INTO settings (key, value) VALUES (sqlc.arg(key), sqlc.arg(value))
ON CONFLICT (key) DO UPDATE SET value = excluded.value;

-- -- audit_logs -------------------------------------

-- name: InsertAuditLog :exec
INSERT INTO audit_logs (id, created_at, actor_id, actor_name, action, entity, entity_id, entity_label, summary,
                        changes, metadata, origin, ip, user_agent, request_id)
VALUES (sqlc.arg(id), sqlc.arg(created_at), sqlc.arg(actor_id), sqlc.arg(actor_name), sqlc.arg(action),
        sqlc.arg(entity), sqlc.arg(entity_id), sqlc.arg(entity_label), sqlc.arg(summary), sqlc.arg(changes),
        sqlc.arg(metadata), sqlc.arg(origin), sqlc.arg(ip), sqlc.arg(user_agent), sqlc.arg(request_id));

-- name: ListAuditLogsDesc :many
SELECT * FROM audit_logs
WHERE (entity = sqlc.arg(entity) OR sqlc.arg(entity) = '')
  AND (action = sqlc.arg(action) OR sqlc.arg(action) = '')
  AND (coalesce(actor_name, '') = sqlc.arg(actor) OR sqlc.arg(actor) = '')
  AND (created_at >= sqlc.arg(from_date) OR sqlc.arg(from_date) = '')
  AND (created_at <= sqlc.arg(to_date) OR sqlc.arg(to_date) = '')
  AND (
    lower(summary) LIKE sqlc.arg(search_like)
    OR sqlc.arg(search_like) = ''
    OR lower(coalesce(entity_label, '')) LIKE sqlc.arg(search_like)
    OR lower(coalesce(actor_name, '')) LIKE sqlc.arg(search_like)
    OR lower(coalesce(entity_id, '')) LIKE sqlc.arg(search_like)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: ListAuditLogsAsc :many
SELECT * FROM audit_logs
WHERE (entity = sqlc.arg(entity) OR sqlc.arg(entity) = '')
  AND (action = sqlc.arg(action) OR sqlc.arg(action) = '')
  AND (coalesce(actor_name, '') = sqlc.arg(actor) OR sqlc.arg(actor) = '')
  AND (created_at >= sqlc.arg(from_date) OR sqlc.arg(from_date) = '')
  AND (created_at <= sqlc.arg(to_date) OR sqlc.arg(to_date) = '')
  AND (
    lower(summary) LIKE sqlc.arg(search_like)
    OR sqlc.arg(search_like) = ''
    OR lower(coalesce(entity_label, '')) LIKE sqlc.arg(search_like)
    OR lower(coalesce(actor_name, '')) LIKE sqlc.arg(search_like)
    OR lower(coalesce(entity_id, '')) LIKE sqlc.arg(search_like)
  )
ORDER BY created_at ASC, id ASC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountAuditLogs :one
SELECT COUNT(*) AS total FROM audit_logs
WHERE (entity = sqlc.arg(entity) OR sqlc.arg(entity) = '')
  AND (action = sqlc.arg(action) OR sqlc.arg(action) = '')
  AND (coalesce(actor_name, '') = sqlc.arg(actor) OR sqlc.arg(actor) = '')
  AND (created_at >= sqlc.arg(from_date) OR sqlc.arg(from_date) = '')
  AND (created_at <= sqlc.arg(to_date) OR sqlc.arg(to_date) = '')
  AND (
    lower(summary) LIKE sqlc.arg(search_like)
    OR sqlc.arg(search_like) = ''
    OR lower(coalesce(entity_label, '')) LIKE sqlc.arg(search_like)
    OR lower(coalesce(actor_name, '')) LIKE sqlc.arg(search_like)
    OR lower(coalesce(entity_id, '')) LIKE sqlc.arg(search_like)
  );

-- name: ListAuditActors :many
SELECT DISTINCT actor_name FROM audit_logs
WHERE actor_name IS NOT NULL AND actor_name <> ''
ORDER BY actor_name ASC;

-- -- whatsapp -------------------------------------

-- name: InsertWhatsAppMessage :execrows
INSERT INTO whatsapp_messages (id, created_at, direction, chat_id, phone, tenant_id, body, status,
                               external_id, metadata)
VALUES (sqlc.arg(id), sqlc.arg(created_at), sqlc.arg(direction), sqlc.arg(chat_id), sqlc.arg(phone),
        sqlc.arg(tenant_id), sqlc.arg(body), sqlc.arg(status), sqlc.arg(external_id),
        sqlc.arg(metadata))
ON CONFLICT (external_id) DO NOTHING;

-- name: ListWhatsAppMessagesByTenant :many
SELECT * FROM whatsapp_messages
WHERE tenant_id = sqlc.arg(tenant_id)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit);

-- inspections ------------------------------------------------------

-- name: ListInspections :many
SELECT i.*, t.name AS tenant_name, p.name AS property_name
FROM inspections i
JOIN leases l ON l.id = i.lease_id
JOIN tenants t ON t.id = l.tenant_id
JOIN properties p ON p.id = l.property_id
ORDER BY i.performed_at DESC, i.created_at DESC;

-- name: GetInspectionByID :one
SELECT i.*, t.name AS tenant_name, p.name AS property_name
FROM inspections i
JOIN leases l ON l.id = i.lease_id
JOIN tenants t ON t.id = l.tenant_id
JOIN properties p ON p.id = l.property_id
WHERE i.id = sqlc.arg(id);

-- name: ListInspectionsByLease :many
SELECT i.*, t.name AS tenant_name, p.name AS property_name
FROM inspections i
JOIN leases l ON l.id = i.lease_id
JOIN tenants t ON t.id = l.tenant_id
JOIN properties p ON p.id = l.property_id
WHERE i.lease_id = sqlc.arg(lease_id)
ORDER BY i.performed_at DESC;

-- name: InsertInspection :exec
INSERT INTO inspections (id, lease_id, kind, performed_at, condition, notes, items, photos, auth_token, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(lease_id), sqlc.arg(kind), sqlc.arg(performed_at), sqlc.arg(condition),
        sqlc.arg(notes), sqlc.arg(items), sqlc.arg(photos), sqlc.arg(auth_token), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: UpdateInspection :exec
UPDATE inspections
SET kind = sqlc.arg(kind), performed_at = sqlc.arg(performed_at), condition = sqlc.arg(condition),
    notes = sqlc.arg(notes), items = sqlc.arg(items), photos = sqlc.arg(photos), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: DeleteInspection :exec
DELETE FROM inspections WHERE id = sqlc.arg(id);

-- jobs -------------------------------------------------------------

-- name: InsertJob :exec
INSERT INTO jobs (id, created_at, updated_at, kind, status, attempts, max_attempts, payload, result, error, scheduled_at, started_at, finished_at)
VALUES (sqlc.arg(id), sqlc.arg(created_at), sqlc.arg(updated_at), sqlc.arg(kind), sqlc.arg(status),
        sqlc.arg(attempts), sqlc.arg(max_attempts), sqlc.arg(payload), sqlc.arg(result), sqlc.arg(error),
        sqlc.arg(scheduled_at), sqlc.arg(started_at), sqlc.arg(finished_at));

-- name: GetJobByID :one
SELECT * FROM jobs WHERE id = sqlc.arg(id);

-- name: ListJobsByStatus :many
SELECT * FROM jobs
WHERE status = sqlc.arg(status)
ORDER BY scheduled_at ASC, created_at ASC
LIMIT sqlc.arg(page_limit);

-- name: ListJobs :many
SELECT * FROM jobs ORDER BY created_at DESC LIMIT sqlc.arg(page_limit);

-- name: UpdateJob :exec
UPDATE jobs
SET updated_at = sqlc.arg(updated_at), status = sqlc.arg(status), attempts = sqlc.arg(attempts),
    result = sqlc.arg(result), error = sqlc.arg(error), scheduled_at = sqlc.arg(scheduled_at),
    started_at = sqlc.arg(started_at), finished_at = sqlc.arg(finished_at)
WHERE id = sqlc.arg(id);

-- payment_proofs ---------------------------------------------------

-- name: InsertPaymentProof :exec
INSERT INTO payment_proofs (id, created_at, updated_at, source, phone, tenant_id, receipt_id, file_name, file_path, mime,
                            extracted_amount, extracted_date, extracted_payer, extracted_payee, extracted_identifier,
                            confidence, status, decision, decision_reason, decided_by, decided_at, raw_response)
VALUES (sqlc.arg(id), sqlc.arg(created_at), sqlc.arg(updated_at), sqlc.arg(source), sqlc.arg(phone), sqlc.arg(tenant_id),
        sqlc.arg(receipt_id), sqlc.arg(file_name), sqlc.arg(file_path), sqlc.arg(mime), sqlc.arg(extracted_amount),
        sqlc.arg(extracted_date), sqlc.arg(extracted_payer), sqlc.arg(extracted_payee), sqlc.arg(extracted_identifier),
        sqlc.arg(confidence), sqlc.arg(status), sqlc.arg(decision), sqlc.arg(decision_reason), sqlc.arg(decided_by),
        sqlc.arg(decided_at), sqlc.arg(raw_response));

-- name: GetPaymentProofByID :one
SELECT * FROM payment_proofs WHERE id = sqlc.arg(id);

-- name: ListPaymentProofsByStatus :many
SELECT * FROM payment_proofs
WHERE status = sqlc.arg(status)
ORDER BY created_at DESC
LIMIT sqlc.arg(page_limit);

-- name: ListPaymentProofs :many
SELECT * FROM payment_proofs ORDER BY created_at DESC LIMIT sqlc.arg(page_limit);

-- name: ListPaymentProofsByReceipt :many
SELECT * FROM payment_proofs WHERE receipt_id = sqlc.arg(receipt_id) ORDER BY created_at DESC;

-- name: UpdatePaymentProofDecision :exec
UPDATE payment_proofs
SET updated_at = sqlc.arg(updated_at), status = sqlc.arg(status), decision = sqlc.arg(decision),
    decision_reason = sqlc.arg(decision_reason), decided_by = sqlc.arg(decided_by), decided_at = sqlc.arg(decided_at),
    receipt_id = sqlc.arg(receipt_id)
WHERE id = sqlc.arg(id);

-- document_intakes -------------------------------------------------

-- name: InsertDocumentIntake :exec
INSERT INTO document_intakes (id, created_at, updated_at, kind, file_name, file_path, mime, status, extracted,
                              confidence, created_entity, created_entity_id, raw_response, notes)
VALUES (sqlc.arg(id), sqlc.arg(created_at), sqlc.arg(updated_at), sqlc.arg(kind), sqlc.arg(file_name),
        sqlc.arg(file_path), sqlc.arg(mime), sqlc.arg(status), sqlc.arg(extracted), sqlc.arg(confidence),
        sqlc.arg(created_entity), sqlc.arg(created_entity_id), sqlc.arg(raw_response), sqlc.arg(notes));

-- name: GetDocumentIntakeByID :one
SELECT * FROM document_intakes WHERE id = sqlc.arg(id);

-- name: ListDocumentIntakes :many
SELECT * FROM document_intakes ORDER BY created_at DESC LIMIT sqlc.arg(page_limit);

-- name: ListDocumentIntakesByStatus :many
SELECT * FROM document_intakes WHERE status = sqlc.arg(status) ORDER BY created_at DESC LIMIT sqlc.arg(page_limit);

-- name: UpdateDocumentIntake :exec
UPDATE document_intakes
SET updated_at = sqlc.arg(updated_at), status = sqlc.arg(status), extracted = sqlc.arg(extracted),
    confidence = sqlc.arg(confidence), created_entity = sqlc.arg(created_entity),
    created_entity_id = sqlc.arg(created_entity_id), raw_response = sqlc.arg(raw_response), notes = sqlc.arg(notes)
WHERE id = sqlc.arg(id);
