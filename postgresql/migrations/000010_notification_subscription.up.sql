CREATE TABLE public.notification_subscriptions (
    id bigserial primary key,
	pubkey bytea NOT NULL,
	url varchar NOT NULL,
	created_at bigint NOT NULL,
	refreshed_at bigint NOT NULL
);

CREATE INDEX notification_subscriptions_pubkey_idx ON public.notification_subscriptions (pubkey);
CREATE UNIQUE INDEX notification_subscriptions_pubkey_url_key ON public.notification_subscriptions (pubkey, url);