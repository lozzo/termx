ALTER TABLE commerce_accounts
  ADD COLUMN operator_role INTEGER NOT NULL DEFAULT 0
  CHECK (operator_role IN (0, 1, 2));
