-- The organisation's subscription: what the payment provider tells us, and what we keep.
-- Split out of queries.sql because it is a responsibility of its own (law 1.4), and because the
-- whole account state -- who may still send, who may no longer sign in -- is read from here.

-- name: UpsertSubscription :exec
INSERT INTO subscriptions (tenant_id, stripe_id, mailbox_range, status)
VALUES ($1, $2, $3, $4)
ON CONFLICT (tenant_id) DO UPDATE SET stripe_id = EXCLUDED.stripe_id, mailbox_range = EXCLUDED.mailbox_range, status = EXCLUDED.status;

-- name: GetSubscription :one
SELECT * FROM subscriptions WHERE tenant_id = $1;

-- name: UpdateSubscriptionStatus :exec
-- Syncs only the status (customer.subscription.updated) without touching the plan/tier.
UPDATE subscriptions SET status = @status WHERE tenant_id = @tenant_id;

-- name: SetSubscriptionPeriodEnd :exec
-- What the customer has paid up to, as the provider last reported it. Kept apart from the status
-- update because the two arrive in different webhooks and neither should erase the other.
UPDATE subscriptions SET current_period_end = @current_period_end WHERE tenant_id = @tenant_id;
