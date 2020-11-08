CREATE TABLE public.payments (
	payment_hash bytea NOT NULL,
	payment_request_out varchar NOT NULL,
	CONSTRAINT payments_pkey PRIMARY KEY (payment_hash)
);