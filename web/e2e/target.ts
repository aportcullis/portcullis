// The e2e stack's throwaway PostgreSQL — the single source of the target coordinates specs dial (minimize-hardcoding: one definition, not one per spec). Pinned in server.sh: PG_PORT and the POSTGRES_* envs there must match.
export type Target = {
  host: string;
  port: number;
  database: string;
  user: string;
  password: string;
};

export const loadTarget = (): Target => ({
  host: "127.0.0.1",
  port: 15432,
  database: "portcullis",
  user: "portcullis",
  password: "portcullis",
});
