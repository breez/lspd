CREATE TABLE public.channels (
	chanid bigint NOT NULL,
	channel_point varchar NULL,
	nodeid bytea NULL,
	CONSTRAINT chanid_pkey PRIMARY KEY (chanid)
);
CREATE INDEX channels_nodeid_idx ON public.channels (nodeid);
