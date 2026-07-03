-- Dev-only (docker compose): the least-privilege login user the server runs as.
-- Migration 0003 creates the portcullis_runtime group role and grants it to this
-- user; migrations themselves keep running as the owner (POSTGRES_USER).
-- initdb scripts run only on an EMPTY data volume — `docker compose down -v`
-- resets it. For production, create an equivalent user yourself and
-- `grant portcullis_runtime to <user>` after the first migration.
create user portcullis_app password 'portcullis';
