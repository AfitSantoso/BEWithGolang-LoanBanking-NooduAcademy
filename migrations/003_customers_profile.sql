-- Additive change for existing Loan_Banking.public.customers.
-- No customer records are deleted or merged. Duplicate identities abort the transaction.
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '20s';

CREATE UNIQUE INDEX IF NOT EXISTS customers_nik_normalized_uidx
    ON public.customers (btrim(nik))
    WHERE nik IS NOT NULL AND btrim(nik) <> '';

COMMIT;
