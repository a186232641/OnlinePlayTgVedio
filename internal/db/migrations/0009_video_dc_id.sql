-- A Telegram document lives on a specific data center (Document.DCID). Streaming
-- and downloading must talk to that DC directly; otherwise the single client has
-- to migrate between DCs per request, which causes IO timeouts under load. Store
-- the DC id so the per-DC connection pool can route file transfers correctly.
-- 0 = unknown (JSON-imported rows resolve it on first play via getMessages).
ALTER TABLE videos ADD COLUMN dc_id INT NOT NULL DEFAULT 0;
