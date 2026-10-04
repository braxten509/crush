-- +goose Up
-- +goose StatementBegin
-- Old binaries ask goose for the schema version before loading any chat.
-- Requiring a registered function here fails closed even in older releases
-- that permit unknown migration numbers. Keep the real history intact.
ALTER TABLE goose_db_version RENAME TO crush_tree_schema_versions;
CREATE VIEW goose_db_version AS
 SELECT id,version_id,is_applied,tstamp FROM crush_tree_schema_versions
 WHERE "Upgrade Crush: this database uses session trees"() = 1;
CREATE TRIGGER tree_schema_version_insert INSTEAD OF INSERT ON goose_db_version BEGIN
 INSERT INTO crush_tree_schema_versions(version_id,is_applied,tstamp)
 VALUES(new.version_id,new.is_applied,coalesce(new.tstamp,datetime('now')));
END;
-- +goose StatementEnd

-- +goose Down
-- No downgrade: old readers cannot represent branches.
