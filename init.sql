CREATE TABLE IF NOT EXISTS public.users
(
    id character varying(32) COLLATE pg_catalog."default" NOT NULL,
    secret character varying(32) COLLATE pg_catalog."default" NOT NULL,
    CONSTRAINT users_pkey PRIMARY KEY (id)
) TABLESPACE pg_default;

CREATE TABLE IF NOT EXISTS public.stations
(
    user_id character varying(32) COLLATE pg_catalog."default" NOT NULL,
    id text COLLATE pg_catalog."default" NOT NULL,
    type text COLLATE pg_catalog."default" NOT NULL,
    source text COLLATE pg_catalog."default",
    CONSTRAINT stations_pkey PRIMARY KEY (user_id, id),
    CONSTRAINT "user" FOREIGN KEY (user_id)
        REFERENCES public.users (id) MATCH SIMPLE
        ON UPDATE NO ACTION
        ON DELETE CASCADE
) TABLESPACE pg_default;

CREATE TABLE IF NOT EXISTS public.bindings
(
    user_id character varying(32) COLLATE pg_catalog."default" NOT NULL,
    id text COLLATE pg_catalog."default" NOT NULL,
    station_user character varying(32) COLLATE pg_catalog."default" NOT NULL,
    station_id text COLLATE pg_catalog."default" NOT NULL,
    CONSTRAINT bindings_pkey PRIMARY KEY (user_id, id)
) TABLESPACE pg_default;