ALTER TABLE public.payments DROP COLUMN payment_request_out;
ALTER TABLE public.payments ADD payment_secret bytea NOT NULL;
ALTER TABLE public.payments ADD destination bytea NOT NULL;
ALTER TABLE public.payments ADD incoming_amount_msat bigint NOT NULL;
ALTER TABLE public.payments ADD outgoing_amount_msat bigint NOT NULL;