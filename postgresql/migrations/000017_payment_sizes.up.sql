ALTER TABLE lsps2.buy_registrations ADD COLUMN params_min_payment_size_msat BIGINT NULL;
ALTER TABLE lsps2.buy_registrations ADD COLUMN params_max_payment_size_msat BIGINT NULL;
UPDATE lsps2.buy_registrations SET params_min_payment_size_msat = 0, params_max_payment_size_msat = 0;
ALTER TABLE lsps2.buy_registrations ALTER COLUMN params_min_payment_size_msat SET NOT NULL;
ALTER TABLE lsps2.buy_registrations ALTER COLUMN params_max_payment_size_msat SET NOT NULL;
