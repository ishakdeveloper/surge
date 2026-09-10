-- A refund the processor could not deliver: the card was closed, or the bank
-- refused it. The money comes back to the platform, the rider still has to be
-- repaid some other way, and everything the refund took — from the payment's
-- refunded total, from the driver's share — is put back.

alter table refund add column if not exists status text not null default 'succeeded';
alter table refund add column if not exists failure_reason text not null default '';
-- The transfer that gave a reversed driver share back once the refund failed.
alter table refund add column if not exists processor_restore_transfer_id text not null default '';

-- refund.failed names the processor's refund id.
create unique index if not exists refund_processor
  on refund (processor_refund_id)
  where processor_refund_id <> '';
