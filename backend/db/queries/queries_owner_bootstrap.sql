-- Self-hosted owner bootstrap (cmd/bootstrap-owner): creates the first organization on an
-- installation that has none yet, then refuses to run again.

-- name: CountTenants :one
SELECT count(*) FROM tenants;
