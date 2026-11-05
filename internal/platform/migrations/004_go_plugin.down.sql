ALTER TABLE plugin_versions RENAME COLUMN library_path TO image_ref;

UPDATE plugin_versions
SET image_ref = 'birkenwald-example:0.1.0'
WHERE name = 'example-check' AND version = '0.1.0';
