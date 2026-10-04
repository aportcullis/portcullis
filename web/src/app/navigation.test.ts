import { describe, expect, it } from "vitest";

import type { PermissionKey } from "@/entities/session/store";
import { landingFor, sections, signInRedirectFor, visibleSections } from "@/app/navigation";


const holder = (...keys: PermissionKey[]) => (key: PermissionKey) => keys.includes(key);

describe("navigation capabilities", () => {
  it("lands a connection administrator on the connections table", () => {
    expect(landingFor(holder("connections.list"))).toBe("/connections");
  });

  it("lands a create-only custom role on requests and shows the section", () => {
    const can = holder("requests.create");
    expect(landingFor(can)).toBe("/requests");
    expect(visibleSections(can).map((section) => section.href)).toEqual(["/requests"]);
  });

  it("shows the requests section to a list-only role", () => {
    const can = holder("requests.list");
    expect(landingFor(can)).toBe("/requests");
    expect(visibleSections(can).map((section) => section.href)).toEqual(["/requests"]);
  });

  it("shows no section to a role holding neither capability", () => {
    const can = holder();
    expect(visibleSections(can)).toHaveLength(0);
  });

  it("hides the requests section from an approve-only role", () => {

    const can = holder("requests.approve");
    expect(visibleSections(can)).toHaveLength(0);
    expect(landingFor(can)).toBe("/requests");
  });

  it("opens the requests section for a create+approve reviewer", () => {
    const can = holder("requests.create", "requests.approve");
    expect(visibleSections(can).map((section) => section.href)).toEqual(["/requests"]);
  });

  it("lands a connections-only role on connections even without any request rights", () => {
    const can = holder("connections.list", "connections.update");
    expect(landingFor(can)).toBe("/connections");
    expect(visibleSections(can).map((section) => section.href)).toEqual(["/connections"]);
  });


  it("shows the connections section to a create-only role", () => {
    const can = holder("connections.create");
    expect(landingFor(can)).toBe("/connections");
    expect(visibleSections(can).map((section) => section.href)).toEqual(["/connections"]);
  });

  it("still shows connections to a list-only role", () => {
    const can = holder("connections.list");
    expect(visibleSections(can).map((section) => section.href)).toEqual(["/connections"]);
  });

  it("hides connections from a role that can only edit or archive", () => {
    // update/delete act on a row the caller must first be able to reach; neither opens the page by itself, exactly as requests.approve does not.
    for (const key of ["connections.update", "connections.delete", "connections.test"] as const) {
      expect(visibleSections(holder(key))).toHaveLength(0);
    }
  });

  it("keeps every section's capability list non-empty", () => {
    // A section with no capabilities would be permanently invisible — a silent dead page rather than a permission decision.
    for (const section of sections) {
      expect(section.capabilities.length).toBeGreaterThan(0);
    }
  });

  it("orders connections before requests when both are held", () => {
    const can = holder("connections.list", "requests.list");
    expect(visibleSections(can).map((section) => section.href)).toEqual(["/connections", "/requests"]);
  });
});

describe("sign-in redirect", () => {
  const at = (pathname: string, search = "", hash = "") => ({ pathname, search, hash });

  it("carries a request detail deep link when no session was found", () => {
    expect(signInRedirectFor("no-session", at("/requests/7f2c"))).toBe("/login?returnTo=%2Frequests%2F7f2c");
  });

  it("carries the composition page when no session was found", () => {
    expect(signInRedirectFor("no-session", at("/requests/new"))).toBe("/login?returnTo=%2Frequests%2Fnew");
  });

  it("carries the query string and fragment of a result page", () => {
    expect(signInRedirectFor("no-session", at("/requests/7f2c/result", "?page=2", "#rows"))).toBe(
      "/login?returnTo=%2Frequests%2F7f2c%2Fresult%3Fpage%3D2%23rows",
    );
  });

  it("carries an unknown address, which still needs sign-in before its not-found page", () => {
    expect(signInRedirectFor("no-session", at("/no-such-page"))).toBe("/login?returnTo=%2Fno-such-page");
  });

  it("starts fresh after an explicit sign-out", () => {
    expect(signInRedirectFor("signed-out", at("/requests/7f2c"))).toBe("/login");
  });

  it("starts fresh from the start page", () => {
    expect(signInRedirectFor("no-session", at("/"))).toBe("/login");
  });

  it("refuses to carry a location that names another host", () => {
    expect(signInRedirectFor("no-session", at("//evil.example/requests"))).toBe("/login");
  });

  it("does not carry the sign-in page itself", () => {
    expect(signInRedirectFor("no-session", at("/login", "?returnTo=%2Frequests"))).toBe("/login");
  });
});
