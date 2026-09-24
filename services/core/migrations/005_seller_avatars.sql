ALTER TABLE seller_profiles ADD COLUMN avatar_mime text;
ALTER TABLE seller_profiles ADD COLUMN avatar_data bytea;
ALTER TABLE seller_profiles ADD COLUMN avatar_version bigint NOT NULL DEFAULT 0;
ALTER TABLE seller_profiles ADD COLUMN public_version bigint NOT NULL DEFAULT 1;
ALTER TABLE seller_profiles ADD CONSTRAINT seller_avatar_pair CHECK (
  (avatar_mime IS NULL AND avatar_data IS NULL) OR
  (avatar_mime IN ('image/png','image/jpeg') AND avatar_data IS NOT NULL AND octet_length(avatar_data) <= 524288)
);
