ALTER TABLE public.payments DROP COLUMN payment_secret;
ALTER TABLE public.payments DROP COLUMN destination;
ALTER TABLE public.payments DROP COLUMN incoming_amount_msat;
ALTER TABLE public.payments DROP COLUMN outgoing_amount_msat;
ALTER TABLE public.payments ADD payment_request_out varchar NOT NULL;