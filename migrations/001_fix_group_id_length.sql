-- Fix group_id field length
ALTER TABLE messages MODIFY COLUMN group_id VARCHAR(128);
