-- +goose Up
CREATE POLICY inbox_retention ON pangolin_events.inbox FOR DELETE TO pangolin_worker USING(state='processed');
GRANT DELETE ON pangolin_events.outbox,pangolin_events.inbox TO pangolin_worker;
-- +goose Down
SELECT 1;
