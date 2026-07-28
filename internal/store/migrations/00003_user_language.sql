-- +goose Up

-- An empty string means "follow the browser", which is the behaviour every
-- existing row had before this column existed. Using '' rather than NULL keeps
-- the column NOT NULL, so reads never have to deal with a null and the default
-- is the same shape as any other value.
--
-- The value is not constrained to a list of locales. A catalog can be removed
-- or renamed, and a preference naming one that no longer ships should degrade
-- to the default rather than make the row unreadable — the read path checks
-- against the loaded catalogs instead.
ALTER TABLE users ADD COLUMN language TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE users DROP COLUMN language;
