import { describe, expect, it } from "vitest";

import { buildLoginHref, returnPathParameter, sanitizeReturnPath } from "@/shared/lib/returnPath";

// A return path is attacker-controllable through a crafted sign-in link, so only same-origin relative paths survive (OWASP unvalidated redirects); everything else lands on the start page.
describe("sanitizeReturnPath", () => {
  it.each([
    ["/requests/new", "/requests/new"],
    ["/requests/7f2c/result?page=2#rows", "/requests/7f2c/result?page=2#rows"],
    ["/connections", "/connections"],
    ["/requests?state=pending", "/requests?state=pending"],
    ["/requests/%2F%2Fevil.com", "/requests/%2F%2Fevil.com"],
  ])("keeps the same-origin path %s", (raw, expected) => {
    expect(sanitizeReturnPath(raw)).toBe(expected);
  });

  it.each([
    ["protocol-relative host", "//evil.example/requests"],
    ["backslash host", "/\\evil.example/requests"],
    ["absolute URL", "https://evil.example/requests"],
    ["script scheme", "javascript:alert(1)"],
    ["tab stripped into a protocol-relative host", "/\t/evil.example/requests"],
    ["newline stripped into a protocol-relative host", "/\n/evil.example/requests"],
    ["bare host without a leading slash", "evil.example/requests"],
    ["empty value", ""],
    ["sign-in loop", "/login?returnTo=/requests"],
    ["bootstrap page", "/bootstrap"],
  ])("refuses a %s", (_kind, raw) => {
    expect(sanitizeReturnPath(raw)).toBe("/");
  });

  it("refuses a missing value", () => {
    expect(sanitizeReturnPath(undefined)).toBe("/");
  });

  it("refuses a repeated parameter instead of choosing one", () => {
    expect(sanitizeReturnPath(["/requests", "//evil.example"])).toBe("/");
  });
});

describe("buildLoginHref", () => {
  it("carries a deep link with its query and fragment", () => {
    const href = buildLoginHref({ pathname: "/requests/7f2c", search: "?tab=sql", hash: "#evidence" });
    const url = new URL(href, "https://portcullis.example");
    expect(url.pathname).toBe("/login");
    expect(url.searchParams.get(returnPathParameter)).toBe("/requests/7f2c?tab=sql#evidence");
  });

  it("round-trips through the sanitizer unchanged", () => {
    const href = buildLoginHref({ pathname: "/requests/new", search: "", hash: "" });
    const carried = new URL(href, "https://portcullis.example").searchParams.get(returnPathParameter) ?? undefined;
    expect(sanitizeReturnPath(carried)).toBe("/requests/new");
  });

  it("omits the parameter for the start page", () => {
    expect(buildLoginHref({ pathname: "/", search: "", hash: "" })).toBe("/login");
  });

  it("omits the parameter for a location the sanitizer would refuse", () => {
    expect(buildLoginHref({ pathname: "//evil.example", search: "", hash: "" })).toBe("/login");
  });
});
