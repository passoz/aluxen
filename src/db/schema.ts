import { sqliteTable, text, integer, real, uniqueIndex } from 'drizzle-orm/sqlite-core';
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
    .notNull()
    .references(() => leases.id, { onDelete: 'cascade' }),
  dueDate: text('due_date').notNull(),
  paidAt: text('paid_at'),
  amount: real('amount').notNull(),
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

// Relacionamentos
export const tenantsRelations = relations(tenants, ({ many }) => ({
  leases: many(leases),
}));

export const propertiesRelations = relations(properties, ({ many }) => ({
  leases: many(leases),
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
