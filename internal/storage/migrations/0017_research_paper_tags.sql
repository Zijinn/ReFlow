-- A paper can wear several labels at once, so the single tag_id reference
-- becomes a join table. Position records the order the user assigned the tags
-- in, which is not the palette order, so it belongs on the association rather
-- than on research_tags.position.
CREATE TABLE research_paper_tags (
    paper_id TEXT NOT NULL,
    tag_id TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (paper_id, tag_id)
);

CREATE INDEX idx_research_paper_tags_tag ON research_paper_tags(tag_id);

-- Carry each paper's one tag over as its first association. Rows whose tag id
-- is missing from research_tags (or belongs to another profile) are dropped:
-- the palette owns the ids, and a foreign association would surface someone
-- else's label.
INSERT INTO research_paper_tags (paper_id, tag_id, position)
SELECT p.id, p.tag_id, 0
FROM research_papers p
JOIN research_tags t ON t.id = p.tag_id AND t.profile_id = p.profile_id
WHERE p.tag_id <> '';

ALTER TABLE research_papers DROP COLUMN tag_id;
