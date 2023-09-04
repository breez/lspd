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
	params_promise varchar NOT NULL,
	token VARCHAR NOT NULL
);
CREATE UNIQUE INDEX idx_lsps2_buy_registrations_scid ON lsps2.buy_registrations (scid);
CREATE INDEX idx_lsps2_buy_registrations_valid_until ON lsps2.buy_registrations (params_valid_until);

CREATE TABLE lsps2.bought_channels (
	id bigserial PRIMARY KEY,
	registration_id bigint NOT NULL,
	funding_tx_id bytea NOT NULL,
	funding_tx_outnum bigint NOT NULL,
	fee_msat bigint NOT NULL,
	payment_size_msat bigint NOT NULL,
	is_completed boolean NOT NULL,
	CONSTRAINT fk_buy_registration
	  FOREIGN KEY(registration_id)
	  REFERENCES lsps2.buy_registrations(id)
	  ON DELETE CASCADE
);
CREATE INDEX idx_lsps2_bought_channels_registration_id ON lsps2.bought_channels (registration_id);

CREATE TABLE lsps2.promises (
	promise varchar PRIMARY KEY,
	token varchar NOT NULL
)