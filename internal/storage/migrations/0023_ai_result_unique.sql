-- The cache key is (profile, entry, operation, language, input hash), but the
-- original index was non-unique, so INSERT OR IGNORE never deduplicated. Keep
-- the newest row per key before enforcing the invariant.
DELETE FROM ai_results
WHERE rowid NOT IN (
    SELECT MAX(rowid)
    FROM ai_results
    GROUP BY profile_id, entry_id, operation, language, input_hash
);

CREATE UNIQUE INDEX idx_ai_results_unique
ON ai_results(profile_id, entry_id, operation, language, input_hash);
