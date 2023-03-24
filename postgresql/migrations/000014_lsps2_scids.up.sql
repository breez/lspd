CREATE TABLE public.scid_reservations (
	id SERIAL PRIMARY KEY,
	lspid bytea NOT NULL,
	scid bigint NOT NULL,
	expiry bigint NOT NULL
);

CREATE UNIQUE INDEX scid_reservations_lspid_scid_idx ON public.scid_reservations (lspid, scid);
CREATE INDEX scid_reservations_expiry_idx ON public.scid_reservations (expiry);
