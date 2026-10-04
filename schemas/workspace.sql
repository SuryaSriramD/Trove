-- Canonical contract migration 0001. Runtime connection owner must also set
-- WAL, busy_timeout and foreign_keys on EACH connection (see service contract).
PRAGMA foreign_keys = ON;
BEGIN IMMEDIATE;
CREATE TABLE workspaces (
 id TEXT PRIMARY KEY, format_version INTEGER NOT NULL CHECK(format_version=1)
);
CREATE TABLE pages (
 id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id),
 parent_page_id TEXT, title TEXT NOT NULL,
 revision TEXT NOT NULL CHECK(revision NOT GLOB '*[^0-9]*' AND substr(revision,1,1) BETWEEN '1' AND '9' AND (length(revision)<19 OR (length(revision)=19 AND revision<='9223372036854775807'))),
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL, deleted_at TEXT,
 provenance_json TEXT CHECK(provenance_json IS NULL OR json_valid(provenance_json)),
 UNIQUE(workspace_id,id), FOREIGN KEY(workspace_id,parent_page_id) REFERENCES pages(workspace_id,id) DEFERRABLE INITIALLY DEFERRED,
 CHECK(parent_page_id IS NULL OR parent_page_id<>id)
);
CREATE TRIGGER page_parent_cycle BEFORE UPDATE OF parent_page_id ON pages WHEN NEW.parent_page_id IS NOT NULL BEGIN
 SELECT RAISE(ABORT,'page_parent_cycle') WHERE EXISTS(
 WITH RECURSIVE ancestors(id) AS (SELECT NEW.parent_page_id UNION SELECT p.parent_page_id FROM pages p JOIN ancestors a ON p.id=a.id WHERE p.parent_page_id IS NOT NULL)
 SELECT 1 FROM ancestors WHERE id=NEW.id);
END;
CREATE TRIGGER page_insert_cycle AFTER INSERT ON pages WHEN NEW.parent_page_id IS NOT NULL BEGIN
 SELECT RAISE(ABORT,'page_parent_cycle') WHERE EXISTS(
 WITH RECURSIVE ancestors(id) AS (SELECT NEW.parent_page_id UNION SELECT p.parent_page_id FROM pages p JOIN ancestors a ON p.id=a.id WHERE p.parent_page_id IS NOT NULL)
 SELECT 1 FROM ancestors WHERE id=NEW.id);
END;
CREATE TABLE blocks (
 id TEXT PRIMARY KEY, page_id TEXT NOT NULL REFERENCES pages(id), parent_block_id TEXT,
 sort_key TEXT NOT NULL CHECK(length(sort_key)=12 AND sort_key NOT GLOB '*[^0-9]*'),
 type TEXT NOT NULL, schema_version INTEGER NOT NULL CHECK(schema_version>=1), data_json TEXT NOT NULL CHECK(json_valid(data_json)),
 revision TEXT NOT NULL CHECK(revision NOT GLOB '*[^0-9]*' AND substr(revision,1,1) BETWEEN '1' AND '9' AND (length(revision)<19 OR (length(revision)=19 AND revision<='9223372036854775807'))),
 UNIQUE(page_id,id), FOREIGN KEY(page_id,parent_block_id) REFERENCES blocks(page_id,id) DEFERRABLE INITIALLY DEFERRED,
 CHECK(parent_block_id IS NULL OR parent_block_id<>id)
);
CREATE UNIQUE INDEX block_order ON blocks(page_id,ifnull(parent_block_id,''),sort_key);
CREATE TRIGGER block_parent_cycle BEFORE UPDATE OF parent_block_id ON blocks WHEN NEW.parent_block_id IS NOT NULL BEGIN
 SELECT RAISE(ABORT,'block_parent_cycle') WHERE EXISTS(
 WITH RECURSIVE ancestors(id) AS (SELECT NEW.parent_block_id UNION SELECT b.parent_block_id FROM blocks b JOIN ancestors a ON b.id=a.id WHERE b.parent_block_id IS NOT NULL)
 SELECT 1 FROM ancestors WHERE id=NEW.id);
END;
CREATE TRIGGER block_insert_cycle AFTER INSERT ON blocks WHEN NEW.parent_block_id IS NOT NULL BEGIN
 SELECT RAISE(ABORT,'block_parent_cycle') WHERE EXISTS(
 WITH RECURSIVE ancestors(id) AS (SELECT NEW.parent_block_id UNION SELECT b.parent_block_id FROM blocks b JOIN ancestors a ON b.id=a.id WHERE b.parent_block_id IS NOT NULL)
 SELECT 1 FROM ancestors WHERE id=NEW.id);
END;
CREATE TABLE collections (
 id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), owning_page_id TEXT NOT NULL,
 title TEXT NOT NULL, title_property_id TEXT NOT NULL, title_type TEXT NOT NULL DEFAULT 'title' CHECK(title_type='title'),
 system_role TEXT NOT NULL DEFAULT 'none' CHECK(system_role IN ('none','tasks')),
 revision TEXT NOT NULL CHECK(revision NOT GLOB '*[^0-9]*' AND substr(revision,1,1) BETWEEN '1' AND '9' AND (length(revision)<19 OR (length(revision)=19 AND revision<='9223372036854775807'))),
 UNIQUE(workspace_id,id), FOREIGN KEY(workspace_id,owning_page_id) REFERENCES pages(workspace_id,id),
 FOREIGN KEY(id,title_property_id,title_type) REFERENCES properties(collection_id,id,type) DEFERRABLE INITIALLY DEFERRED
);
CREATE UNIQUE INDEX one_tasks_collection ON collections(workspace_id) WHERE system_role='tasks';
CREATE TABLE properties (
 id TEXT PRIMARY KEY, collection_id TEXT NOT NULL REFERENCES collections(id), name TEXT NOT NULL,
 type TEXT NOT NULL, schema_version INTEGER NOT NULL CHECK(schema_version>=1), config_json TEXT NOT NULL CHECK(json_valid(config_json)),
 revision TEXT NOT NULL CHECK(revision NOT GLOB '*[^0-9]*' AND substr(revision,1,1) BETWEEN '1' AND '9' AND (length(revision)<19 OR (length(revision)=19 AND revision<='9223372036854775807'))),
 UNIQUE(collection_id,id,type), UNIQUE(collection_id,id)
);
CREATE UNIQUE INDEX one_title_property ON properties(collection_id) WHERE type='title';
CREATE TABLE property_options (
 id TEXT PRIMARY KEY, property_id TEXT NOT NULL REFERENCES properties(id), label TEXT NOT NULL,
 status_group TEXT CHECK(status_group IN ('not_started','in_progress','done')),
 UNIQUE(property_id,id)
);
CREATE TABLE records (
 id TEXT PRIMARY KEY, collection_id TEXT NOT NULL REFERENCES collections(id), page_id TEXT NOT NULL UNIQUE REFERENCES pages(id),
 sort_key TEXT NOT NULL CHECK(length(sort_key)=12 AND sort_key NOT GLOB '*[^0-9]*'),
 revision TEXT NOT NULL CHECK(revision NOT GLOB '*[^0-9]*' AND substr(revision,1,1) BETWEEN '1' AND '9' AND (length(revision)<19 OR (length(revision)=19 AND revision<='9223372036854775807'))),
 UNIQUE(collection_id,id), UNIQUE(collection_id,sort_key)
);
CREATE TRIGGER record_workspace_insert BEFORE INSERT ON records BEGIN
 SELECT RAISE(ABORT,'record_workspace_mismatch') WHERE (SELECT workspace_id FROM pages WHERE id=NEW.page_id)<>(SELECT workspace_id FROM collections WHERE id=NEW.collection_id);
END;
CREATE TRIGGER record_workspace_update BEFORE UPDATE OF collection_id,page_id ON records BEGIN
 SELECT RAISE(ABORT,'record_workspace_mismatch') WHERE (SELECT workspace_id FROM pages WHERE id=NEW.page_id)<>(SELECT workspace_id FROM collections WHERE id=NEW.collection_id);
END;
CREATE TABLE property_values (
 record_id TEXT NOT NULL, collection_id TEXT NOT NULL, property_id TEXT NOT NULL, property_type TEXT NOT NULL CHECK(property_type<>'title'),
 schema_version INTEGER NOT NULL CHECK(schema_version>=1), value_json TEXT NOT NULL CHECK(json_valid(value_json)),
 status_option_id TEXT GENERATED ALWAYS AS (CASE WHEN property_type='status' THEN json_extract(value_json,'$') END) STORED,
 PRIMARY KEY(record_id,property_id),
 FOREIGN KEY(collection_id,record_id) REFERENCES records(collection_id,id),
 FOREIGN KEY(collection_id,property_id,property_type) REFERENCES properties(collection_id,id,type),
 FOREIGN KEY(property_id,status_option_id) REFERENCES property_options(property_id,id),
 CHECK(property_type<>'status' OR (json_type(value_json)='text' AND status_option_id IS NOT NULL))
);
CREATE TABLE task_facets (
 record_id TEXT PRIMARY KEY, collection_id TEXT NOT NULL, status_property_id TEXT NOT NULL,
 status_type TEXT NOT NULL DEFAULT 'status' CHECK(status_type='status'), due_property_id TEXT,
 due_type TEXT NOT NULL DEFAULT 'date' CHECK(due_type='date'),
 FOREIGN KEY(collection_id,record_id) REFERENCES records(collection_id,id),
 FOREIGN KEY(collection_id,status_property_id,status_type) REFERENCES properties(collection_id,id,type),
 FOREIGN KEY(collection_id,due_property_id,due_type) REFERENCES properties(collection_id,id,type),
 FOREIGN KEY(record_id,status_property_id) REFERENCES property_values(record_id,property_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE VIEW task_state AS SELECT t.record_id,p.title,v.status_option_id, o.status_group, o.status_group='done' AS completed
 FROM task_facets t JOIN records r ON r.id=t.record_id JOIN pages p ON p.id=r.page_id
 JOIN property_values v ON v.record_id=t.record_id AND v.property_id=t.status_property_id
 JOIN property_options o ON o.id=v.status_option_id;
CREATE TABLE todo_references (
 block_id TEXT PRIMARY KEY REFERENCES blocks(id), task_record_id TEXT NOT NULL REFERENCES task_facets(record_id)
);
CREATE TRIGGER todo_shape_insert BEFORE INSERT ON blocks WHEN NEW.type='todo' BEGIN
 SELECT RAISE(ABORT,'todo_shape') WHERE json_type(NEW.data_json,'$.taskRecordId') IS NOT 'text' OR (SELECT count(*) FROM json_each(NEW.data_json))<>1;
END;
CREATE TRIGGER todo_reference_insert AFTER INSERT ON blocks WHEN NEW.type='todo' BEGIN
 INSERT INTO todo_references VALUES(NEW.id,json_extract(NEW.data_json,'$.taskRecordId'));
END;
CREATE TRIGGER todo_shape_update BEFORE UPDATE OF type,data_json ON blocks WHEN NEW.type='todo' BEGIN
 SELECT RAISE(ABORT,'todo_shape') WHERE json_type(NEW.data_json,'$.taskRecordId') IS NOT 'text' OR (SELECT count(*) FROM json_each(NEW.data_json))<>1;
END;
CREATE TRIGGER todo_reference_update AFTER UPDATE OF type,data_json ON blocks BEGIN
 DELETE FROM todo_references WHERE block_id=NEW.id;
 INSERT INTO todo_references SELECT NEW.id,json_extract(NEW.data_json,'$.taskRecordId') WHERE NEW.type='todo';
END;
CREATE TABLE containers (
 id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), owning_page_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('database','project')), label TEXT NOT NULL, parent_project_id TEXT,
 revision TEXT NOT NULL CHECK(revision NOT GLOB '*[^0-9]*' AND substr(revision,1,1) BETWEEN '1' AND '9' AND (length(revision)<19 OR (length(revision)=19 AND revision<='9223372036854775807'))),
 FOREIGN KEY(workspace_id,owning_page_id) REFERENCES pages(workspace_id,id), UNIQUE(workspace_id,id),
 FOREIGN KEY(workspace_id,parent_project_id) REFERENCES containers(workspace_id,id), CHECK(parent_project_id IS NULL OR parent_project_id<>id)
);
CREATE TABLE container_collections (
 container_id TEXT NOT NULL REFERENCES containers(id), collection_id TEXT NOT NULL REFERENCES collections(id),
 PRIMARY KEY(container_id,collection_id)
);
CREATE TABLE views (
 id TEXT PRIMARY KEY, collection_id TEXT NOT NULL REFERENCES collections(id), container_id TEXT,
 owning_page_id TEXT NOT NULL REFERENCES pages(id), name TEXT NOT NULL, type TEXT NOT NULL,
 schema_version INTEGER NOT NULL CHECK(schema_version>=1), query_json TEXT NOT NULL CHECK(json_valid(query_json)), config_json TEXT NOT NULL CHECK(json_valid(config_json)),
 revision TEXT NOT NULL CHECK(revision NOT GLOB '*[^0-9]*' AND substr(revision,1,1) BETWEEN '1' AND '9' AND (length(revision)<19 OR (length(revision)=19 AND revision<='9223372036854775807'))),
 FOREIGN KEY(container_id,collection_id) REFERENCES container_collections(container_id,collection_id)
);
CREATE TABLE files (
 id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), content_hash TEXT NOT NULL CHECK(length(content_hash)=64 AND content_hash NOT GLOB '*[^0-9a-f]*'),
 original_name TEXT NOT NULL, media_type TEXT NOT NULL, size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),
 storage_key TEXT NOT NULL CHECK(storage_key='files/'||substr(content_hash,1,2)||'/'||content_hash),
 revision TEXT NOT NULL CHECK(revision NOT GLOB '*[^0-9]*' AND substr(revision,1,1) BETWEEN '1' AND '9' AND (length(revision)<19 OR (length(revision)=19 AND revision<='9223372036854775807'))),
 UNIQUE(workspace_id,content_hash)
);
CREATE TABLE attachments (
 page_id TEXT NOT NULL REFERENCES pages(id), block_id TEXT, file_id TEXT NOT NULL REFERENCES files(id),
 PRIMARY KEY(page_id,file_id), FOREIGN KEY(page_id,block_id) REFERENCES blocks(page_id,id)
);
CREATE TABLE meetings (
 id TEXT PRIMARY KEY, page_id TEXT NOT NULL UNIQUE REFERENCES pages(id), recording_file_id TEXT REFERENCES files(id), started_at TEXT NOT NULL,
 duration_ms INTEGER NOT NULL CHECK(duration_ms>=0), state TEXT NOT NULL CHECK(state IN ('recording','captured','transcribing','ready','failed','cancelled')),
 revision TEXT NOT NULL CHECK(revision NOT GLOB '*[^0-9]*' AND substr(revision,1,1) BETWEEN '1' AND '9' AND (length(revision)<19 OR (length(revision)=19 AND revision<='9223372036854775807')))
);
CREATE TABLE transcript_segments (
 id TEXT PRIMARY KEY, meeting_id TEXT NOT NULL REFERENCES meetings(id), block_id TEXT NOT NULL UNIQUE REFERENCES blocks(id),
 start_ms INTEGER NOT NULL CHECK(start_ms>=0), end_ms INTEGER NOT NULL CHECK(end_ms>=start_ms), text TEXT NOT NULL, speaker_label TEXT,
 revision TEXT NOT NULL CHECK(revision NOT GLOB '*[^0-9]*' AND substr(revision,1,1) BETWEEN '1' AND '9' AND (length(revision)<19 OR (length(revision)=19 AND revision<='9223372036854775807')))
);
CREATE TABLE project_scopes (
 project_id TEXT PRIMARY KEY REFERENCES containers(id), inherit_ancestor_preferences INTEGER NOT NULL DEFAULT 0 CHECK(inherit_ancestor_preferences IN (0,1)),
 inherit_workspace_preferences INTEGER NOT NULL DEFAULT 0 CHECK(inherit_workspace_preferences IN (0,1)),
 revision TEXT NOT NULL CHECK(revision NOT GLOB '*[^0-9]*' AND substr(revision,1,1) BETWEEN '1' AND '9' AND (length(revision)<19 OR (length(revision)=19 AND revision<='9223372036854775807')))
);
CREATE TABLE project_pages (
 project_id TEXT NOT NULL REFERENCES project_scopes(project_id), page_id TEXT NOT NULL REFERENCES pages(id),
 relationship TEXT NOT NULL CHECK(relationship IN ('member','pin','exclude')), PRIMARY KEY(project_id,page_id,relationship)
);
CREATE TABLE memory_entries (
 id TEXT PRIMARY KEY, page_id TEXT NOT NULL UNIQUE REFERENCES pages(id), scope_kind TEXT NOT NULL CHECK(scope_kind IN ('workspace','project','pages')),
 scope_json TEXT NOT NULL CHECK(json_valid(scope_json)), kind TEXT NOT NULL CHECK(kind IN ('preference','project_context','decision')),
 status TEXT NOT NULL CHECK(status IN ('proposed','active','disabled','stale','superseded','forgotten')), content TEXT, page_ref TEXT REFERENCES pages(id),
 revision TEXT NOT NULL CHECK(revision NOT GLOB '*[^0-9]*' AND substr(revision,1,1) BETWEEN '1' AND '9' AND (length(revision)<19 OR (length(revision)=19 AND revision<='9223372036854775807'))),
 approved_at TEXT, approved_by_actor_id TEXT, supersedes_id TEXT REFERENCES memory_entries(id),
 CHECK(status<>'active' OR (approved_at IS NOT NULL AND approved_by_actor_id IS NOT NULL)),
 CHECK((content IS NULL) <> (page_ref IS NULL)),
 CHECK((kind='project_context' AND page_ref IS NOT NULL) OR (kind<>'project_context' AND content IS NOT NULL)),
 CHECK(supersedes_id IS NULL OR supersedes_id<>id)
);
CREATE TABLE memory_sources (
 memory_id TEXT NOT NULL REFERENCES memory_entries(id), page_id TEXT NOT NULL REFERENCES pages(id), source_revision TEXT NOT NULL CHECK(source_revision NOT GLOB '*[^0-9]*' AND substr(source_revision,1,1) BETWEEN '1' AND '9' AND (length(source_revision)<19 OR (length(source_revision)=19 AND source_revision<='9223372036854775807'))),
 block_id TEXT, meeting_id TEXT REFERENCES meetings(id), segment_id TEXT REFERENCES transcript_segments(id), start_ms INTEGER, end_ms INTEGER,
 source_index INTEGER NOT NULL CHECK(source_index>=0),
 PRIMARY KEY(memory_id,source_index), FOREIGN KEY(page_id,block_id) REFERENCES blocks(page_id,id),
 CHECK((meeting_id IS NULL AND segment_id IS NULL AND start_ms IS NULL AND end_ms IS NULL) OR (meeting_id IS NOT NULL AND segment_id IS NOT NULL AND block_id IS NOT NULL AND start_ms>=0 AND end_ms>=start_ms))
);
-- Source invalidation is a workspace-service mutation with revision/history/event
-- updates. Retrieval MUST also check live source revisions before returning memory.
CREATE TABLE import_mappings (
 id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), source_system TEXT NOT NULL, source_namespace TEXT NOT NULL,
 external_kind TEXT NOT NULL, identity_kind TEXT NOT NULL CHECK(identity_kind IN ('external_id','derived_locator')), identity_value TEXT NOT NULL,
 local_kind TEXT NOT NULL, local_id TEXT NOT NULL, base_source_revision TEXT NOT NULL, base_content_hash TEXT NOT NULL, base_raw_file_id TEXT NOT NULL REFERENCES files(id), last_import_run_id TEXT NOT NULL,
 UNIQUE(workspace_id,source_system,source_namespace,external_kind,identity_kind,identity_value)
);
CREATE TABLE import_observations (
 mapping_id TEXT NOT NULL REFERENCES import_mappings(id), import_run_id TEXT NOT NULL, source_revision TEXT NOT NULL,
 raw_file_id TEXT NOT NULL REFERENCES files(id), result TEXT NOT NULL CHECK(result IN ('preserved','adapted','partial','unsupported_but_retained','failed')),
 PRIMARY KEY(mapping_id,import_run_id)
);
-- Schema code is a reviewable persistence contract, not a license for adapters to write SQL.
PRAGMA user_version = 1;
COMMIT;
