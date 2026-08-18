-- What a model suggested about an intake submission.
--
-- A column rather than a table, because there is exactly one suggestion per
-- submission and a join to fetch it would buy nothing.
--
-- Stored beside the submission and *not* applied to the task. That is the whole
-- design: the model suggests, a person confirms. The automation engine already
-- carries the reasoning in its own comments - a rule engine running arbitrary
-- expressions is a scripting environment without any of the safeguards of one -
-- and a model writing directly to tasks would be a less predictable version of
-- the same problem. It would also be unauditable, since nobody could tell later
-- which values a human chose and which arrived on their own.
ALTER TABLE intake_submissions
    ADD COLUMN suggestion   jsonb,
    -- When the model answered. NULL means it has not been asked yet, which is
    -- what the sweep looks for.
    ADD COLUMN suggested_at timestamptz,
    -- Set when somebody applies the suggestion to the task. Kept so the trail
    -- distinguishes "the model proposed this" from "a person agreed", which is
    -- the only way to tell afterwards whether the suggestions are any good.
    ADD COLUMN accepted_at  timestamptz,
    ADD COLUMN accepted_by  uuid REFERENCES users (id) ON DELETE SET NULL;

-- The sweep asks for submissions nobody has triaged, newest first, so the index
-- covers exactly that and nothing else.
CREATE INDEX intake_submissions_untriaged_idx
    ON intake_submissions (created_at)
    WHERE suggested_at IS NULL;
