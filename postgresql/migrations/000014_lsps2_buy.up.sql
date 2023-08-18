CREATE SCHEMA lsps2;
CREATE TABLE lsps2.buy_registrations (
    id bigserial PRIMARY KEY, 
    lsp_id bytea NOT NULL,
    peer_id bytea NOT NULL,
    scid bigint NOT NULL,
	mode smallint NOT NULL,
	payment_size_msat bigint NULL,
    params_min_fee_msat bigint NOT NULL,
	params_proportional bigint NOT NULL,
	params_valid_until varchar NOT NULL,
	params_min_lifetime bigint NOT NULL,
	params_max_client_to_self_delay bigint NOT NULL,
	params_promise varchar NOT NULL
);
CREATE UNIQUE INDEX idx_lsps2_buy_registrations_scid ON lsps2.buy_registrations (scid);
CREATE INDEX idx_lsps2_buy_registrations_valid_until ON lsps2.buy_registrations (params_valid_until);
