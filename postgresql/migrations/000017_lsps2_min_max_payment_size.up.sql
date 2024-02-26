ALTER TABLE lsps2.buy_registrations 
ADD COLUMN params_min_payment_size_msat bigint NULL,
ADD COLUMN params_max_payment_size_msat bigint NULL;

UPDATE lsps2.buy_registrations
SET params_min_payment_size_msat = 0, params_max_payment_size_msat = 0;

ALTER TABLE lsps2.buy_registrations
ALTER COLUMN params_min_payment_size_msat SET NOT NULL,
ALTER COLUMN params_max_payment_size_msat SET NOT NULL;

ALTER TABLE public.new_channel_params
ADD COLUMN min_fee_msat bigint NULL,
ADD COLUMN proportional bigint NULL,
ADD COLUMN min_lifetime bigint NULL,
ADD COLUMN max_client_to_self_delay bigint NULL,
ADD COLUMN min_payment_size_msat bigint NULL,
ADD COLUMN max_payment_size_msat bigint NULL;

UPDATE public.new_channel_params
SET min_fee_msat = (params::json->>'min_msat')::bigint
,   proportional = (params::json->>'proportional')::bigint
,   min_lifetime = (params::json->>'max_idle_time')::bigint
,   max_client_to_self_delay = (params::json->>'max_client_to_self_delay')::bigint
,   min_payment_size_msat = 1000
,   max_payment_size_msat = 4000000000;

ALTER TABLE public.new_channel_params
ALTER COLUMN min_fee_msat SET NOT NULL,
ALTER COLUMN proportional SET NOT NULL,
ALTER COLUMN min_lifetime SET NOT NULL,
ALTER COLUMN max_client_to_self_delay SET NOT NULL,
ALTER COLUMN min_payment_size_msat SET NOT NULL,
ALTER COLUMN max_payment_size_msat SET NOT NULL,
DROP COLUMN params;
