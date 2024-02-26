ALTER TABLE lsps2.buy_registrations
DROP COLUMN params_min_payment_size_msat,
DROP COLUMN params_max_payment_size_msat;

ALTER TABLE public.new_channel_params
ADD COLUMN params jsonb NULL;

UPDATE public.new_channel_params a
SET params = to_jsonb(c)
FROM (
    SELECT b.min_fee_msat::varchar AS min_msat
    ,      b.proportional
    ,      b.min_lifetime AS max_idle_time
    ,      b.max_client_to_self_delay
    FROM public.new_channel_params b
    WHERE a.token = b.token AND a.validity = b.validity
) c;

ALTER TABLE public.new_channel_params
DROP COLUMN min_fee_msat,
DROP COLUMN proportional,
DROP COLUMN min_lifetime,
DROP COLUMN max_client_to_self_delay,
DROP COLUMN min_payment_size_msat,
DROP COLUMN max_payment_size_msat
ALTER COLUMN params SET NOT NULL;