-- Sellers who haven't said yes to news are asked once, on their dashboard.
ALTER TABLE marketing_contacts DROP CONSTRAINT IF EXISTS marketing_contacts_source;
ALTER TABLE marketing_contacts ADD CONSTRAINT marketing_contacts_source
  CHECK (source IN ('seller_signup','booking','offer','settings','dashboard','unsubscribe_link','ops_import'));
