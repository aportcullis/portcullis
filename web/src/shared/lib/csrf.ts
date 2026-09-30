const CSRF_COOKIE = "__Host-portcullis_csrf";

/** Reads the CSRF token cookie, returning an empty string when absent. */
export function readCSRFTokenCookie(): string {
  const match = document.cookie.match(
    new RegExp(`(?:^|;\\s*)${CSRF_COOKIE.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}=([^;]*)`),
  );
  return match ? decodeURIComponent(match[1]) : "";
}
