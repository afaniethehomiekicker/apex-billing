-- APEX Billing: give the app its own limited database user.
--
-- Why: connecting as "postgres" gives the app (and anyone who gets its
-- password from .env) full control of EVERY database on the server,
-- including APEX CORE's. This user can only use the apex_billing database.
--
-- How (once, as postgres, after stopping apex-billing.exe):
--   1. Change the password below to a long random one.
--   2. Run:  psql -U postgres -f scripts\db-setup.sql
--   3. In .env set DB_USER=apex_billing_app and DB_PASSWORD=<that password>.
--   4. Start apex-billing.exe again.
-- Safe to run again (it skips what already exists).

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'apex_billing_app') THEN
    CREATE ROLE apex_billing_app LOGIN PASSWORD 'CHANGE-THIS-to-a-long-random-password'
      NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
  END IF;
END $$;

-- Create the database if it doesn't exist yet (first install).
SELECT 'CREATE DATABASE apex_billing OWNER apex_billing_app'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'apex_billing') \gexec

ALTER DATABASE apex_billing OWNER TO apex_billing_app;
-- Nobody else may even connect to it.
REVOKE ALL ON DATABASE apex_billing FROM PUBLIC;
GRANT CONNECT, TEMPORARY ON DATABASE apex_billing TO apex_billing_app;

\connect apex_billing

GRANT USAGE, CREATE ON SCHEMA public TO apex_billing_app;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;

-- Hand over the tables, sequences and functions made so far.
DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
    EXECUTE format('ALTER TABLE public.%I OWNER TO apex_billing_app', r.tablename);
  END LOOP;
  FOR r IN SELECT sequence_name FROM information_schema.sequences WHERE sequence_schema = 'public' LOOP
    EXECUTE format('ALTER SEQUENCE public.%I OWNER TO apex_billing_app', r.sequence_name);
  END LOOP;
  FOR r IN SELECT p.oid::regprocedure AS fn FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
           WHERE n.nspname = 'public' AND p.proname LIKE 'apex_%' LOOP
    EXECUTE format('ALTER FUNCTION %s OWNER TO apex_billing_app', r.fn);
  END LOOP;
END $$;
