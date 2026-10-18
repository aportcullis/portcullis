/** Reads a single opaque token from a fragment without accepting query-string delivery. */
export function passwordSetupToken(fragment: string): string {
  if (!fragment.startsWith("#")) return "";
  const parameters = new URLSearchParams(fragment.slice(1));
  const values = parameters.getAll("token");
  const token = values.length === 1 ? values[0] : "";
  return token !== undefined && /^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$/.test(token) ? token : "";
}

/** Builds a private setup link from the application's own browser origin. */
export function passwordSetupLink(origin: string, token: string): string { const link = new URL("/setup-password", origin); link.hash = new URLSearchParams({ token }).toString(); return link.href; }
