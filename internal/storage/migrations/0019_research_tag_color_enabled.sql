-- A colour is not always the point of a label. Some tags group papers rather than
-- rank them, and a grouping tag should not wash its row pink. `color` keeps the tint
-- the user picked ('' still means "follow the palette order") and this flag says
-- whether to paint with it at all.
--
-- A second column rather than a ninth sentinel value inside `color`, so that turning
-- the tint off and back on returns the chip to exactly the colour it had instead of
-- silently dropping it to the palette default.
ALTER TABLE research_tags ADD COLUMN color_enabled INTEGER NOT NULL DEFAULT 1;
