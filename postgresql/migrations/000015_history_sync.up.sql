ALTER INDEX public.channels_nodeid_idx RENAME TO channels_backup_nodeid_idx;
ALTER TABLE public.channels RENAME TO channels_backup;
ALTER INDEX public.forwarding_history_chanid_in_idx RENAME TO forwarding_history_backup_chanid_in_idx;
ALTER INDEX public.forwarding_history_chanid_out_idx RENAME TO forwarding_history_backup_chanid_out_idx;
ALTER TABLE public.forwarding_history RENAME TO forwarding_history_backup;

CREATE TABLE public.channels (
	nodeid bytea NOT NULL,
    peerid bytea NOT NULL,
    alias_scid bigint NULL,
	confirmed_scid bigint NULL,
	funding_tx_id bytea NOT NULL,
	funding_tx_outnum bigint NOT NULL,
    first_seen timestamp NOT NULL,
	last_update timestamp NOT NULL,
    UNIQUE(nodeid, funding_tx_id, funding_tx_outnum)
);
CREATE INDEX channels_nodeid_idx ON public.channels USING btree (nodeid);
CREATE INDEX channels_peerid_idx ON public.channels USING btree (peerid);
CREATE INDEX channels_funding_tx_idx ON public.channels USING btree (funding_tx_id, funding_tx_outnum);
CREATE INDEX channels_nodeid_funding_tx_idx ON public.channels USING btree (nodeid, funding_tx_id, funding_tx_outnum);
CREATE INDEX channels_alias_scid_idx ON public.channels USING btree (alias_scid);
CREATE INDEX channels_confirmed_scid_idx ON public.channels USING btree (confirmed_scid);

CREATE TABLE public.forwarding_history (
    identifier varchar NOT NULL,
    resolved_time bigint NOT NULL,
    nodeid bytea NOT NULL,
	chanid_in bigint NOT NULL,
    chanid_out bigint NOT NULL,
    amt_msat_in bigint NOT NULL,
    amt_msat_out bigint NOT NULL,
    UNIQUE(nodeid, identifier)
);
CREATE INDEX forwarding_history_nodeid_idx ON public.forwarding_history (nodeid);
CREATE INDEX forwarding_history_nodeid_resolved_time_idx ON public.forwarding_history USING btree (nodeid, resolved_time);
CREATE INDEX forwarding_history_resolved_time_idx ON public.forwarding_history USING btree (resolved_time);
CREATE INDEX forwarding_history_nodeid_chanid_in_idx ON public.forwarding_history USING btree (nodeid, chanid_in);
CREATE INDEX forwarding_history_nodeid_chanid_out_idx ON public.forwarding_history USING btree (nodeid, chanid_out);

CREATE TABLE public.cln_forwarding_history_offsets (
    nodeid bytea NOT NULL,
    last_created_index bigint NOT NULL,
    last_updated_index bigint NOT NULL,
    UNIQUE(nodeid)
);

CREATE INDEX payments_funding_tx_idx ON public.payments USING btree (funding_tx_id, funding_tx_outnum);
CREATE INDEX lsps2_bought_channels_registration_id_funding_tx_idx ON lsps2.bought_channels USING btree (registration_id, funding_tx_id, funding_tx_outnum);
