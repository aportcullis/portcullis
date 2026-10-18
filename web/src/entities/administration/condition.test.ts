import { describe, expect, it } from "vitest";

import { buildCondition, type ConditionChoices } from "@/entities/administration/condition";

const choices: ConditionChoices = { title: "Temporary access", startsAt: "", expiresAt: "", resourceType: "", resourceID: "" };

describe("form-generated conditional grants", () => {
  it("builds an expiry in an explicit UTC contract", () => {
    expect(buildCondition({ ...choices, expiresAt: "2030-01-01T12:00:00Z" }).expression).toBe('request.time < timestamp("2030-01-01T12:00:00.000Z")');
  });
  it("combines a window and an exact resource with conjunction", () => {
    const source = buildCondition({ ...choices, startsAt: "2029-01-01T00:00:00Z", expiresAt: "2030-01-01T00:00:00Z", resourceType: "request", resourceID: "ebdd035a-7045-4474-b115-f49a16d49a41" });
    expect(source.expression).toContain('resource.type == "request" && resource.id == "ebdd035a-7045-4474-b115-f49a16d49a41"');
    expect(source.expression).toContain(" && request.time < timestamp(");
    expect(source.languageVersion).toBe("cel-v1");
  });
  it("accepts a type-only restriction without broadening it", () => {
    expect(buildCondition({ ...choices, resourceType: "connection" }).expression).toBe('resource.type == "connection"');
  });
  it("normalizes explicit offsets while retaining the title", () => {
    const source = buildCondition({ ...choices, expiresAt: "2030-01-01T21:00:00+09:00" });
    expect(source.title).toBe(choices.title);
    expect(source.expression).toContain('2030-01-01T12:00:00.000Z');
  });
  it.each([
    choices,
    { ...choices, title: "", resourceType: "request" },
    { ...choices, expiresAt: "not-a-date" },
    { ...choices, expiresAt: "2030-02-30T12:00:00Z" },
    { ...choices, expiresAt: "2030-01-01T12:00" },
    { ...choices, startsAt: "2031-01-01T00:00:00Z", expiresAt: "2030-01-01T00:00:00Z" },
    { ...choices, resourceType: 'request" || true' },
    { ...choices, resourceID: 'bad" || true' },
  ])("refuses invalid choices instead of producing unconditional access", invalid => {
    expect(() => buildCondition(invalid)).toThrow();
  });
});
