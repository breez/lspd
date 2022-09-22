ALTER INDEX public.channels_nodeid_idx RENAME TO channels_new_nodeid_idx;
ALTER INDEX public.channels_channel_point_pkey RENAME TO channels_new_channel_point_pkey
ALTER TABLE public.channels RENAME TO channels_new;

CREATE TABLE public.channels (
	chanid int8 NOT NULL,
	channel_point varchar NULL,
	nodeid bytea NULL,
	last_update timestamp NULL,
	CONSTRAINT chanid_pkey PRIMARY KEY (chanid)
);
CREATE INDEX channels_nodeid_idx ON public.channels USING btree (nodeid);

INSERT INTO public.channels
SELECT initial_chanid chanid, channel_point, nodeid, last_update FROM channels_new;

INSERT INTO public.channels
SELECT confirmed_chanid chanid, channel_point, nodeid, last_update FROM channels_new
    WHERE confirmed_chanid IS NOT NULL AND confirmed_chanid <> initial_chanid;

DROP TABLE channels_new;
