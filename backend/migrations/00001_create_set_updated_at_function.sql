-- set_updated_at() is a shared trigger function that stamps updated_at on
-- every UPDATE. Future tables attach it with:
--   CREATE TRIGGER <table>_set_updated_at BEFORE UPDATE ON <table>
--       FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- No CASCADE: rolling back fails loudly while any trigger still uses this
-- function, instead of silently dropping those triggers.
DROP FUNCTION IF EXISTS set_updated_at();
