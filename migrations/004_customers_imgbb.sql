-- Additive ImgBB metadata on Loan_Banking.public.customers.
-- Existing *_image_path columns retain the direct image URL; no data is deleted.
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '20s';
ALTER TABLE public.customers
 ADD COLUMN IF NOT EXISTS ktp_image_id VARCHAR(64),
 ADD COLUMN IF NOT EXISTS selfie_image_id VARCHAR(64),
 ADD COLUMN IF NOT EXISTS payslip_image_id VARCHAR(64);
COMMIT;
