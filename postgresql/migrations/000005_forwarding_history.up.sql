CREATE TABLE public.forwarding_history (
    "timestamp" bigint NOT NULL,
	chanid_in bigint NOT NULL,
    chanid_out bigint NOT NULL,
    amt_msat_in bigint NOT NULL,
    amt_msat_out bigint NOT NULL,
	CONSTRAINT timestamp_pkey PRIMARY KEY ("timestamp")
);
CREATE INDEX forwarding_history_chanid_in_idx ON public.forwarding_history (chanid_in);
CREATE INDEX forwarding_history_chanid_out_idx ON public.forwarding_history (chanid_out);