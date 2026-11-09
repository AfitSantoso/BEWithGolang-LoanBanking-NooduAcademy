-- Record of registration/RBAC changes for Neon database Loan_Banking.
-- The users table stores passwords in password_hash.
-- Existing usernames and account data are preserved; existing accounts get role user.
BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '20s';
LOCK TABLE public.users IN SHARE ROW EXCLUSIVE MODE;

ALTER TABLE public.users
    ADD COLUMN IF NOT EXISTS username VARCHAR(50);

UPDATE public.users SET username = 'user_' || id::text WHERE username IS NULL;

ALTER TABLE public.users ALTER COLUMN username SET NOT NULL;

ALTER TABLE public.users
    ADD COLUMN IF NOT EXISTS role VARCHAR(20) NOT NULL DEFAULT 'user';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'public.users'::regclass AND conname = 'users_role_check'
    ) THEN
        ALTER TABLE public.users ADD CONSTRAINT users_role_check CHECK (role IN ('user', 'admin'));
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS users_email_normalized_uidx
    ON public.users (lower(btrim(email)));
CREATE UNIQUE INDEX IF NOT EXISTS users_username_normalized_uidx
    ON public.users (lower(btrim(username)));

-- Imported/manual IDs may be ahead of the sequence. Never move it backwards.
DO $$
DECLARE
    sequence_name regclass;
    sequence_value bigint;
    maximum_id bigint;
BEGIN
    sequence_name := pg_get_serial_sequence('public.users', 'id')::regclass;
    SELECT max(id) INTO maximum_id FROM public.users;
    IF sequence_name IS NOT NULL AND maximum_id IS NOT NULL THEN
        EXECUTE format('SELECT last_value FROM %s', sequence_name) INTO sequence_value;
        PERFORM setval(sequence_name, GREATEST(sequence_value, maximum_id), true);
    END IF;
END $$;

COMMIT;
