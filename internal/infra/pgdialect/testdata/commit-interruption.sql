CREATE FUNCTION public.delay_insert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_sleep(1);
    RETURN NEW;
END
$$;
CREATE TRIGGER exec_t_delay_insert BEFORE INSERT ON public.exec_t
FOR EACH ROW EXECUTE FUNCTION public.delay_insert();
CREATE FUNCTION public.block_commit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock('public.exec_t'::regclass::oid::bigint);
    RETURN NULL;
END
$$;
CREATE CONSTRAINT TRIGGER exec_t_block_commit AFTER INSERT ON public.exec_t
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.block_commit();
