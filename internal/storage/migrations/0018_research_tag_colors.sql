-- A tag chip used to take its colour from the row's index in the palette, so
-- dragging one label re-tinted every chip below it and a user could never say
-- "High is red". The colour is stored per tag instead: '' means the user has
-- not chosen one and the client falls back to index-derived tinting, while any
-- other value is one of the eight names the stylesheet ships (red, amber, gray,
-- green, teal, orange, violet, blue). The whitelist lives in the storage layer,
-- because a name the CSS does not know would render as an unstyled chip.
ALTER TABLE research_tags ADD COLUMN color TEXT NOT NULL DEFAULT '';

-- A fresh install gets the three priority labels the workbench is designed
-- around. Only profiles whose palette is still empty are seeded, so a user who
-- has already built their own labels is never handed a second, competing set.
-- The ids are minted in SQL exactly like migration 0016 does it: these rows are
-- written before any Go code sees them, so there is no uuid to reuse.
INSERT INTO research_tags (id, profile_id, name, position, color, created_at, updated_at)
SELECT lower(hex(randomblob(16))), seeded.profile_id, seeded.name, seeded.position, seeded.color,
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM (
    SELECT p.id AS profile_id, label.name, label.position, label.color
    FROM profiles p
    CROSS JOIN (
        SELECT 'High' AS name, 0 AS position, 'red' AS color
        UNION ALL SELECT 'Medium', 1, 'amber'
        UNION ALL SELECT 'Low', 2, 'green'
    ) label
    -- An empty palette is the whole condition, so the name guard below cannot
    -- fire today; it is kept because UNIQUE (profile_id, name) would otherwise
    -- abort the migration the moment someone relaxes this filter, and a hand-
    -- made "High" label must survive rather than collide.
    WHERE NOT EXISTS (
        SELECT 1 FROM research_tags t WHERE t.profile_id = p.id
    )
) seeded
WHERE NOT EXISTS (
    SELECT 1 FROM research_tags t
    WHERE t.profile_id = seeded.profile_id AND t.name = seeded.name
);
