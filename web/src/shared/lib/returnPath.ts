/** The search parameter that carries where to continue after signing in. */
export const returnPathParameter = "returnTo";

const startPath = "/";
// Parsing against a placeholder origin shows whether a browser would leave this application for the value.
const placeholderOrigin = "https://portcullis.invalid";
// Signing in must never continue to a page that sends the caller back to sign-in or first-run setup.
const unauthenticatedPaths: readonly string[] = ["/login", "/bootstrap"];

/** Reports whether the value holds an ASCII control character or space, which URL parsers strip or reinterpret. */
function hasControlOrSpace(value: string): boolean {
  return Array.from(value).some((character) => {
    const codePoint = character.codePointAt(0) ?? 0;
    return codePoint <= 0x20 || codePoint === 0x7f;
  });
}

/** Reports whether the value is not a single-slash path on this origin; "//host" and "/\host" are host references. */
function isHostReference(path: string): boolean {
  return !path.startsWith("/") || path.startsWith("//") || path.startsWith("/\\");
}

/** Returns the requested same-origin path to continue at after sign-in, or "/" when it is absent or unsafe. */
export function sanitizeReturnPath(raw: string | string[] | undefined): string {
  if (typeof raw !== "string") return startPath;
  if (isHostReference(raw)) return startPath;
  if (hasControlOrSpace(raw)) return startPath;
  const parsed = new URL(raw, placeholderOrigin);
  if (parsed.origin !== placeholderOrigin) return startPath;
  // Dot segments are resolved during parsing, so "/.//host" only becomes a host reference afterwards; check the path that would be used.
  if (isHostReference(parsed.pathname)) return startPath;
  if (unauthenticatedPaths.includes(parsed.pathname)) return startPath;
  return `${parsed.pathname}${parsed.search}${parsed.hash}`;
}

/** Builds the sign-in address that returns to the given location afterwards. */
export function buildLoginHref(location: { pathname: string; search: string; hash: string }): string {
  const returnPath = sanitizeReturnPath(`${location.pathname}${location.search}${location.hash}`);
  if (returnPath === startPath) return "/login";
  return `/login?${new URLSearchParams({ [returnPathParameter]: returnPath }).toString()}`;
}
