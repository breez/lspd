CREATE TABLE public.new_channel_params (
	validity int NOT NULL,
	params jsonb NOT NULL
);
CREATE UNIQUE INDEX new_channel_params_validity_idx ON public.new_channel_params (validity);

INSERT INTO public.new_channel_params (validity, params)
 VALUES(259200, '{"min_msat": "12000000", "proportional": 7500, "max_idle_time": 4320, "max_client_to_self_delay": 432, "min_payment_size_msat": "1000", "max_payment_size_msat": "4000000000"}'::jsonb);

INSERT INTO public.new_channel_params (validity, params)
 VALUES(3600, '{"min_msat": "10000000", "proportional": 7500, "max_idle_time": 4320, "max_client_to_self_delay": 432, "min_payment_size_msat": "1000", "max_payment_size_msat": "4000000000"}'::jsonb);
