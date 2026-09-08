PRAGMA foreign_keys=OFF;--> statement-breakpoint
CREATE TABLE `__new_receipts` (
	`id` text PRIMARY KEY NOT NULL,
	`lease_id` text,
	`tenant_id` text,
	`property_id` text,
	`category` text DEFAULT 'rent' NOT NULL,
	`description` text,
	`due_date` text NOT NULL,
	`paid_at` text,
	`amount` real NOT NULL,
	`paid_amount` real,
	`pdf_path` text,
	`generated_key` text,
	`auth_token` text,
	`created_at` text DEFAULT (datetime('now')) NOT NULL,
	`updated_at` text,
	FOREIGN KEY (`lease_id`) REFERENCES `leases`(`id`) ON UPDATE no action ON DELETE cascade,
	FOREIGN KEY (`tenant_id`) REFERENCES `tenants`(`id`) ON UPDATE no action ON DELETE set null,
	FOREIGN KEY (`property_id`) REFERENCES `properties`(`id`) ON UPDATE no action ON DELETE set null
);
--> statement-breakpoint
INSERT INTO `__new_receipts`("id", "lease_id", "due_date", "paid_at", "amount", "pdf_path", "generated_key", "auth_token", "created_at", "updated_at") SELECT "id", "lease_id", "due_date", "paid_at", "amount", "pdf_path", "generated_key", "auth_token", "created_at", "updated_at" FROM `receipts`;--> statement-breakpoint
DROP TABLE `receipts`;--> statement-breakpoint
ALTER TABLE `__new_receipts` RENAME TO `receipts`;--> statement-breakpoint
PRAGMA foreign_keys=ON;--> statement-breakpoint
CREATE UNIQUE INDEX `receipts_generated_key_idx` ON `receipts` (`generated_key`);