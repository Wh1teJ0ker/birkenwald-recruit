ALTER TABLE plugin_versions RENAME COLUMN image_ref TO library_path;

UPDATE plugin_versions
SET library_path = 'plugins/example/example.so'
WHERE name = 'example-check' AND version = '0.1.0';
