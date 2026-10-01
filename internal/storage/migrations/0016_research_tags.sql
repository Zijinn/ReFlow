-- User-defined tags replace the fixed High/Medium/Average priority enum. The
-- paper column becomes a tag id reference and the palette gets its own table,
-- so labels can be created, renamed and ordered per profile; papers keep ids
-- instead of names so a rename never has to rewrite paper rows.
CREATE TABLE research_tags (
    id TEXT PRIMARY KEY,
    profile_id TEXT NOT NULL,
    name TEXT NOT NULL,
    position INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (profile_id, name)
);

CREATE INDEX idx_research_tags_profile ON research_tags(profile_id, position);

ALTER TABLE research_papers ADD COLUMN tag_id TEXT NOT NULL DEFAULT '';

-- Seed the palette from the values already in use, keeping the old priority
-- order (High > Medium > Average > anything else) as the tag order.
INSERT INTO research_tags (id, profile_id, name, position, created_at, updated_at)
SELECT lower(hex(randomblob(16))), profile_id, priority,
       ROW_NUMBER() OVER (
           PARTITION BY profile_id
           ORDER BY CASE priority
               WHEN 'High' THEN 0
               WHEN 'Medium' THEN 1
               WHEN 'Average' THEN 2
               ELSE 3
           END, priority
       ) - 1,
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM (SELECT DISTINCT profile_id, priority FROM research_papers WHERE priority <> '');

UPDATE research_papers
SET tag_id = (
    SELECT t.id FROM research_tags t
    WHERE t.profile_id = research_papers.profile_id AND t.name = research_papers.priority
)
WHERE priority <> '';

ALTER TABLE research_papers DROP COLUMN priority;
