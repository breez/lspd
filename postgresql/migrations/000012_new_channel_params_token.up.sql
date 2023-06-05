ALTER TABLE public.new_channel_params ADD token varchar;
DROP INDEX public.new_channel_params_validity_idx;
CREATE UNIQUE INDEX new_channel_params_token_validity_idx ON public.new_channel_params (token, validity);