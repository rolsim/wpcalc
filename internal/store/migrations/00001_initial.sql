-- +goose Up

-- Dates are stored as ISO TEXT ('YYYY-MM-DD') rather than as SQLite's numeric
-- date types. They sort and compare lexicographically in exactly calendar
-- order, they survive a sqlite3 shell dump unambiguously, and they cannot pick
-- up a timezone offset on the way in or out.
--
-- The GLOB checks are deliberate belt-and-braces: the domain layer already
-- refuses malformed dates, but this table will outlive any one version of that
-- code, and a bad date here silently drops rows out of month queries.

CREATE TABLE employees (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    display_name TEXT    NOT NULL CHECK (length(trim(display_name)) > 0),
    start_date   TEXT    NOT NULL CHECK (start_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    end_date     TEXT        NULL CHECK (end_date IS NULL OR end_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT    NOT NULL DEFAULT (datetime('now')),

    -- A zero-length employment is not a thing; equal dates mean one day.
    CHECK (end_date IS NULL OR end_date >= start_date)
);

-- Hours live as integer hundredths ("industrial minutes"): 7.75 h is 775.
-- Summing these along both grid axes and again in the PDFs must agree exactly,
-- which REAL cannot guarantee. The upper bound matches domain.MaxDailyCentihours.
CREATE TABLE time_entries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    employee_id INTEGER NOT NULL REFERENCES employees (id) ON DELETE CASCADE,
    work_date   TEXT    NOT NULL CHECK (work_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    centihours  INTEGER NOT NULL CHECK (centihours >= 0 AND centihours <= 2400),
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT    NOT NULL DEFAULT (datetime('now')),

    -- One cell in the grid is one row here.
    UNIQUE (employee_id, work_date)
);

-- Every read is "one month of entries", so the range scan wants work_date first.
CREATE INDEX idx_time_entries_date ON time_entries (work_date, employee_id);

-- One comment per calendar day, belonging to the day rather than to any
-- employee — it is the row header note, not a per-cell annotation.
CREATE TABLE day_comments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    work_date  TEXT NOT NULL UNIQUE CHECK (work_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    comment    TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- +goose Down

DROP INDEX IF EXISTS idx_time_entries_date;
DROP TABLE IF EXISTS day_comments;
DROP TABLE IF EXISTS time_entries;
DROP TABLE IF EXISTS employees;
