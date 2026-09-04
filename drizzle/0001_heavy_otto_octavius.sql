ALTER TABLE `leases` ADD `auth_token` text;--> statement-breakpoint
ALTER TABLE `receipts` ADD `auth_token` text;--> statement-breakpoint
CREATE UNIQUE INDEX IF NOT EXISTS `receipts_generated_key_idx` ON `receipts` (`generated_key`);