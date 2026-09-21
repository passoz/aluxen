import { sqliteTable, text, integer, real, uniqueIndex, index } from 'drizzle-orm/sqlite-core';
import { relations, sql } from 'drizzle-orm';

export const users = sqliteTable('users', {
  id: text('id').primaryKey(),
  username: text('username').notNull().unique(),
  passwordHash: text('password_hash').notNull(),
  createdAt: text('created_at').notNull().default(sql`(datetime('now'))`),
});

export const tenants = sqliteTable('tenants', {
  id: text('id').primaryKey(),
  name: text('name').notNull(),
  email: text('email').notNull(),
  phone: text('phone'),
  cpf: text('cpf'),
  /**
   * Credencial da Área do Cliente. `passwordHash` nulo significa que o locatário
   * ainda usa a senha padrão (o próprio CPF, sem pontuação); `passwordChangedAt`
   * nulo marca que a troca obrigatória do primeiro acesso ainda está pendente.
   */
  passwordHash: text('password_hash'),
  passwordChangedAt: text('password_changed_at'),
  createdAt: text('created_at').notNull().default(sql`(datetime('now'))`),
  updatedAt: text('updated_at').notNull().default(sql`(datetime('now'))`),
});

export const properties = sqliteTable('properties', {
  id: text('id').primaryKey(),
  name: text('name').notNull(),
  address: text('address').notNull(),
  city: text('city').notNull(),
  state: text('state').notNull(),
  zip: text('zip'),
  createdAt: text('created_at').notNull().default(sql`(datetime('now'))`),
  updatedAt: text('updated_at').notNull().default(sql`(datetime('now'))`),
});

export const leases = sqliteTable('leases', {
  id: text('id').primaryKey(),
  tenantId: text('tenant_id')
    .notNull()
    .references(() => tenants.id, { onDelete: 'cascade' }),
  propertyId: text('property_id')
    .notNull()
    .references(() => properties.id, { onDelete: 'cascade' }),
  startDate: text('start_date').notNull(),
  endDate: text('end_date').notNull(),
  dueDay: integer('due_day').notNull().default(1),
  rentAmount: real('rent_amount').notNull(),
  status: text('status').notNull().default('active'),
  authToken: text('auth_token'),
  createdAt: text('created_at').notNull().default(sql`(datetime('now'))`),
  updatedAt: text('updated_at').notNull().default(sql`(datetime('now'))`),
});

export const receipts = sqliteTable('receipts', {
  id: text('id').primaryKey(),
  leaseId: text('lease_id')
    .references(() => leases.id, { onDelete: 'cascade' }),
  tenantId: text('tenant_id')
    .references(() => tenants.id, { onDelete: 'set null' }),
  propertyId: text('property_id')
    .references(() => properties.id, { onDelete: 'set null' }),
  category: text('category').notNull().default('rent'),
  description: text('description'),
  dueDate: text('due_date').notNull(),
  paidAt: text('paid_at'),
  amount: real('amount').notNull(),
  paidAmount: real('paid_amount'),
  pdfPath: text('pdf_path'),
  generatedKey: text('generated_key'),
  authToken: text('auth_token'),
  createdAt: text('created_at').notNull().default(sql`(datetime('now'))`),
  updatedAt: text('updated_at'),
}, (table) => [
  uniqueIndex('receipts_generated_key_idx').on(table.generatedKey),
]);

export const settings = sqliteTable('settings', {
  key: text('key').primaryKey(),
  value: text('value').notNull(),
});

/**
 * Trilha de auditoria — registro imutável de tudo que muda o estado do sistema
 * (cadastros, valores, contratos, emissões de documentos, acessos e backups).
 *
 * `changes` guarda um JSON `[{ field, label, from, to }]` com valores já
 * formatados para leitura humana; `metadata` guarda contexto adicional.
 */
export const auditLogs = sqliteTable('audit_logs', {
  id: text('id').primaryKey(),
  createdAt: text('created_at').notNull(),
  actorId: text('actor_id'),
  actorName: text('actor_name'),
  action: text('action').notNull(),
  entity: text('entity').notNull(),
  entityId: text('entity_id'),
  entityLabel: text('entity_label'),
  summary: text('summary').notNull(),
  changes: text('changes'),
  metadata: text('metadata'),
  origin: text('origin').notNull().default('web'),
  ip: text('ip'),
  userAgent: text('user_agent'),
  requestId: text('request_id'),
}, (table) => [
  index('audit_logs_created_at_idx').on(table.createdAt),
  index('audit_logs_entity_idx').on(table.entity, table.entityId),
  index('audit_logs_action_idx').on(table.action),
]);

// Relacionamentos
export const tenantsRelations = relations(tenants, ({ many }) => ({
  leases: many(leases),
  receipts: many(receipts),
}));

export const propertiesRelations = relations(properties, ({ many }) => ({
  leases: many(leases),
  receipts: many(receipts),
}));

export const leasesRelations = relations(leases, ({ one, many }) => ({
  tenant: one(tenants, {
    fields: [leases.tenantId],
    references: [tenants.id],
  }),
  property: one(properties, {
    fields: [leases.propertyId],
    references: [properties.id],
  }),
  receipts: many(receipts),
}));

export const receiptsRelations = relations(receipts, ({ one }) => ({
  lease: one(leases, {
    fields: [receipts.leaseId],
    references: [leases.id],
  }),
  tenant: one(tenants, {
    fields: [receipts.tenantId],
    references: [tenants.id],
  }),
  property: one(properties, {
    fields: [receipts.propertyId],
    references: [properties.id],
  }),
}));

export type User = typeof users.$inferSelect;
export type NewUser = typeof users.$inferInsert;
export type Tenant = typeof tenants.$inferSelect;
export type NewTenant = typeof tenants.$inferInsert;
export type Property = typeof properties.$inferSelect;
export type NewProperty = typeof properties.$inferInsert;
export type Lease = typeof leases.$inferSelect;
export type NewLease = typeof leases.$inferInsert;
export type Receipt = typeof receipts.$inferSelect;
export type NewReceipt = typeof receipts.$inferInsert;
export type Setting = typeof settings.$inferSelect;
export type AuditLog = typeof auditLogs.$inferSelect;
export type NewAuditLog = typeof auditLogs.$inferInsert;
