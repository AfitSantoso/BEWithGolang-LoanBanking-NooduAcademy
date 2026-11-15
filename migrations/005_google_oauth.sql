-- Additive Google OAuth identity and one-use browser-bound login state.
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '20s';
ALTER TABLE public.users ADD COLUMN IF NOT EXISTS google_subject VARCHAR(255);
CREATE UNIQUE INDEX IF NOT EXISTS users_google_subject_uidx ON public.users(google_subject) WHERE google_subject IS NOT NULL;
CREATE TABLE IF NOT EXISTS public.google_oauth_states (
 state_hash VARCHAR(64) PRIMARY KEY,
 binding_hash VARCHAR(64) NOT NULL,
 nonce VARCHAR(128) NOT NULL,
 verifier VARCHAR(128) NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 link_user_id INTEGER REFERENCES public.users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS google_oauth_states_expiry_idx ON public.google_oauth_states(expires_at);
COMMIT;
