ALTER INDEX public.channels_nodeid_idx RENAME TO channels_backup_nodeid_idx;
ALTER TABLE public.channels RENAME TO channels_backup;
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

ALTER INDEX public.forwarding_history_chanid_in_idx RENAME TO forwarding_history_backup_chanid_in_idx;
ALTER INDEX public.forwarding_history_chanid_out_idx RENAME TO forwarding_history_backup_chanid_out_idx;
ALTER TABLE public.forwarding_history RENAME TO forwarding_history_backup;
CREATE TABLE public.forwarding_history (
    identifier varchar NOT NULL,
    resolved_time bigint NOT NULL,
    nodeid bytea NOT NULL,
	chanid_in bigint NOT NULL,
    chanid_out bigint NOT NULL,
    amt_msat_in bigint NOT NULL,
    amt_msat_out bigint NOT NULL,
    funding_tx_id_in bytea NULL,
    funding_tx_outnum_in bigint NULL,
    funding_tx_id_out bytea NULL,
    funding_tx_outnum_out bigint NULL,
    forward_correlation_in uuid NULL,
    forward_correlation_out uuid NULL,
    UNIQUE(nodeid, identifier)
);
CREATE INDEX forwarding_history_nodeid_idx ON public.forwarding_history (nodeid);
CREATE INDEX forwarding_history_nodeid_resolved_time_idx ON public.forwarding_history USING btree (nodeid, resolved_time);
CREATE INDEX forwarding_history_resolved_time_idx ON public.forwarding_history USING btree (resolved_time);
CREATE INDEX forwarding_history_nodeid_chanid_in_idx ON public.forwarding_history USING btree (nodeid, chanid_in);
CREATE INDEX forwarding_history_nodeid_chanid_out_idx ON public.forwarding_history USING btree (nodeid, chanid_out);
CREATE INDEX forwarding_history_nodeid_funding_tx_in_idx ON public.forwarding_history USING btree (nodeid, funding_tx_id_in, funding_tx_outnum_in);
CREATE INDEX forwarding_history_nodeid_funding_tx_out_idx ON public.forwarding_history USING btree (nodeid, funding_tx_id_out, funding_tx_outnum_out);
CREATE INDEX forwarding_history_forward_correlation_in_idx ON public.forwarding_history USING btree (forward_correlation_in);
CREATE INDEX forwarding_history_forward_correlation_out_idx ON public.forwarding_history USING btree (forward_correlation_out);

CREATE TABLE public.cln_forwarding_history_offsets (
    nodeid bytea NOT NULL,
    last_created_index bigint NOT NULL,
    last_updated_index bigint NOT NULL,
    UNIQUE(nodeid)
);

CREATE TABLE public.external_token_forwards (
    id uuid NOT NULL,
    nodeid bytea NOT NULL,
    external_nodeid bytea NOT NULL,
    token varchar NOT NULL,
    resolved_time bigint NOT NULL,
    direction varchar NOT NULL,
    amount_msat bigint NOT NULL,
    forward_correlation uuid NULL,
    UNIQUE(nodeid, external_nodeid, direction, resolved_time, amount_msat)
);

CREATE TABLE public.forward_correlations (
    id uuid NOT NULL,
    internal_remote_forward_nodeid bytea NULL,
    internal_remote_forward_identifier varchar NULL,
    external_remote_forward_id uuid NULL
);
CREATE INDEX forward_correlations_internal_remote_forward_idx ON public.forward_correlations USING btree (internal_remote_forward_nodeid, internal_remote_forward_identifier);
CREATE INDEX forward_correlations_external_remote_forward_idx ON public.forward_correlations USING btree (external_remote_forward_id);

CREATE INDEX payments_funding_tx_idx ON public.payments USING btree (funding_tx_id, funding_tx_outnum);
CREATE INDEX lsps2_bought_channels_registration_id_funding_tx_idx ON lsps2.bought_channels USING btree (registration_id, funding_tx_id, funding_tx_outnum);
