-- Plan catalog read by the product, with caching.

-- name: ListPlans :many
SELECT * FROM plans ORDER BY sort, key;

-- name: GetPlan :one
SELECT * FROM plans WHERE key = @key;
