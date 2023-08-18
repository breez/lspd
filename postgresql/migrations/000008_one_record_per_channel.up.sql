ALTER INDEX public.channels_nodeid_idx RENAME TO channels_old_nodeid_idx;
ALTER INDEX public.chanid_pkey RENAME TO channels_old_chanid_pkey;
ALTER TABLE public.channels RENAME TO channels_old;

CREATE TABLE public.channels (
	initial_chanid int8 NOT NULL,
	confirmed_chanid int8 NULL,
	channel_point varchar NOT NULL,
	nodeid bytea NOT NULL,
	last_update timestamp NULL,
	CONSTRAINT channels_channel_point_pkey PRIMARY KEY (channel_point)
);
CREATE INDEX channels_nodeid_idx ON public.channels USING btree (nodeid);

INSERT INTO public.channels
SELECT
 min(chanid) initial_chanid,
 CASE WHEN (max(chanid) >> 40) < (3 << 17) THEN NULL ELSE max(chanid) END confirmed_chanid,
 channel_point, nodeid, max(last_update) last_update
FROM channels_old GROUP BY channel_point, nodeid;

DROP TABLE public.channels_old;
