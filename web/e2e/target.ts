// The local test harness serves its owned database coordinates after Testcontainers readiness.
export type Target = {
  host: string;
  port: number;
  database: string;
  user: string;
  password: string;
};

/** Reports whether a decoded harness response carries every database coordinate with its expected type. */
function isTarget(value: unknown): value is Target {
  return typeof value === "object" && value !== null
    && "host" in value && typeof value.host === "string"
    && "port" in value && typeof value.port === "number"
    && "database" in value && typeof value.database === "string"
    && "user" in value && typeof value.user === "string"
    && "password" in value && typeof value.password === "string";
}

export const loadTarget = async (): Promise<Target> => {
  const response = await fetch("http://127.0.0.1:18081/target", { signal: AbortSignal.timeout(5_000) });
  if (!response.ok) throw new Error("Testcontainers target unavailable");
  const decoded: unknown = await response.json();
  if (!isTarget(decoded)) throw new Error("Testcontainers target response is malformed");
  return decoded;
};
