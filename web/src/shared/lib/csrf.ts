// The server issues the CSRF token in a JS-readable __Host- cookie; every
// authenticated RPC must echo it in the X-CSRF-Token header (HMAC double
// submit, ADR-0006). The cookie name is pinned by the transport layer.
const CSRF_COOKIE = "__Host-portcullis_csrf";

// csrfToken returns the current CSRF cookie value, or "" before login.
export function csrfToken(): string {
  const match = document.cookie.match(
    new RegExp(`(?:^|;\\s*)${CSRF_COOKIE.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}=([^;]*)`),
  );
  return match ? decodeURIComponent(match[1]) : "";
}
