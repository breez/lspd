CREATE TABLE public.open_channel_htlcs (
	nodeid bytea NOT NULL,
    peerid bytea NOT NULL,
	funding_tx_id bytea NOT NULL,
	funding_tx_outnum bigint NOT NULL,
    forward_amt_msat bigint NOT NULL,
    original_amt_msat bigint NOT NULL,
    forward_time bigint NOT NULL
);