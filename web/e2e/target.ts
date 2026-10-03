// The local test harness serves its owned database coordinates after Testcontainers readiness.
export type Target = {
  host: string;
  port: number;
  database: string;
  user: string;
  password: string;
};

export const loadTarget = async (): Promise<Target> => {
  const response = await fetch("http://127.0.0.1:18081/target", { signal: AbortSignal.timeout(5_000) });
  if (!response.ok) throw new Error("Testcontainers target unavailable");
  return await response.json() as Target;
};
