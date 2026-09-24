ALTER TABLE seller_profiles
  ADD COLUMN identity_url text,
  ADD CONSTRAINT seller_identity_url_shape CHECK (
    identity_url IS NULL OR
    (identity_url LIKE 'https://%' AND length(identity_url) <= 512 AND identity_url !~ '[[:cntrl:]]')
  );
