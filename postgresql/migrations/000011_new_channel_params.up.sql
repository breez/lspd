CREATE TABLE public.new_channel_params (
	validity int NOT NULL,
	params jsonb NOT NULL
);
CREATE UNIQUE INDEX new_channel_params_validity_idx ON public.new_channel_params (validity);
