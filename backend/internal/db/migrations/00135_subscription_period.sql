-- +goose Up
-- What the customer has already paid for, so the block can be decided without asking Stripe.
--
-- WHY IT HAS TO BE STORED. Whether an organisation may still send is asked on every message and on
-- every page load. Answering it with a call to Stripe would put their availability in front of our
-- customers' mail -- and on the day Stripe is slow, nobody sends anything. The webhook already
-- tells us when this changes; writing it down is one column.
--
-- NULL means no paid period is known: either they never subscribed (the trial applies, counted from
-- the organisation's own creation date) or the provider never told us. Neither is a reason to block
-- anybody, so the code treats NULL as "no deadline from here".
ALTER TABLE subscriptions ADD COLUMN current_period_end TIMESTAMPTZ;

-- +goose Down
ALTER TABLE subscriptions DROP COLUMN IF EXISTS current_period_end;
