import { describe, expect, it } from "vitest";

import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import {
  appendDraftParameter,
  createEmptyRequestDraft,
  resolveUnavailableTargetRequestState,
  isSelectedTargetAvailable,
  resolveUnavailableTargetAction,
  removeDraftParameter,
  updateDraftParameter,
  toTypedRequestParameters,
  validateRequestDraft,
  isParameterValueDisabled,
} from "@/features/request/draft";

describe("request draft", () => {
  it("adds, patches, and removes parameter rows immutably", () => {
    const a = appendDraftParameter(createEmptyRequestDraft());
    expect(a.params).toHaveLength(1);
    const b = updateDraftParameter(a, 0, { name: "id", type: "integer", value: "7" });
    expect(a.params[0].name).toBe("");
    expect(b.params[0]).toEqual({ name: "id", type: "integer", value: "7" });
    const c = removeDraftParameter(b, 0);
    expect(c.params).toHaveLength(0);
  });

  it("decides what a lost target means from the request's actual state", () => {

    expect(resolveUnavailableTargetRequestState(AccessRequestState.CANCELLED)).toBe("already-settled");
    expect(resolveUnavailableTargetRequestState(AccessRequestState.EXPIRED)).toBe("already-settled");
    expect(resolveUnavailableTargetRequestState(AccessRequestState.REJECTED)).toBe("already-settled");
    expect(resolveUnavailableTargetRequestState(AccessRequestState.DRAFT)).toBe("still-cancellable");

    expect(resolveUnavailableTargetRequestState(AccessRequestState.PENDING)).toBe("already-settled");
  });

  it("tells apart the two ways a chosen connection can go missing", () => {
    // Before saving, the form still owns its choice: clear it and let the user pick another. After saving, the draft is BOUND to that connection (the select is disabled — §4.4 makes the target immutable), so "pick another one" is advice the UI cannot honour; the honest answer is that this draft can no longer be submitted, with a way out.
    expect(resolveUnavailableTargetAction(false)).toBe("repick");
    expect(resolveUnavailableTargetAction(true)).toBe("unsubmittable");
  });

  it("reports whether a chosen connection survived a refetch", () => {
    const list = [{ id: "conn-1" }, { id: "conn-2" }];

    expect(isSelectedTargetAvailable(list, "")).toBe(true);
    expect(isSelectedTargetAvailable(list, "conn-2")).toBe(true);
    // The target was archived between opening the dialog and submitting: the refetched list no longer offers it, so the selection must be dropped rather than silently submitted against a dead connection.
    expect(isSelectedTargetAvailable(list, "conn-gone")).toBe(false);
    // An empty list cannot honour any choice.
    expect(isSelectedTargetAvailable([], "conn-1")).toBe(false);

    expect(isSelectedTargetAvailable([], "")).toBe(true);
  });

  it("requires non-empty SQL", () => {
    expect(validateRequestDraft({ sql: "   ", params: [] })).toMatch(/SQL/);
    expect(validateRequestDraft({ sql: "select 1", params: [] })).toBe("");
  });

  it("rejects unnamed and duplicate parameters", () => {
    expect(validateRequestDraft({ sql: "select :a", params: [{ name: "", type: "string", value: "x" }] })).toMatch(
      /needs a name/,
    );
    expect(
      validateRequestDraft({
        sql: "select :a",
        params: [
          { name: "a", type: "string", value: "1" },
          { name: "a", type: "string", value: "2" },
        ],
      }),
    ).toMatch(/more than once/);
  });

  it("validates typed values", () => {
    expect(validateRequestDraft({ sql: "s", params: [{ name: "n", type: "integer", value: "seven" }] })).toMatch(
      /whole number/,
    );
    expect(validateRequestDraft({ sql: "s", params: [{ name: "n", type: "integer", value: "7" }] })).toBe("");
    expect(validateRequestDraft({ sql: "s", params: [{ name: "b", type: "boolean", value: "yes" }] })).toMatch(
      /true or false/,
    );
    expect(validateRequestDraft({ sql: "s", params: [{ name: "u", type: "uuid", value: "not-a-uuid" }] })).toMatch(
      /UUID/,
    );
  });

  it("treats null parameters as valueless", () => {
    expect(isParameterValueDisabled("null")).toBe(true);
    expect(isParameterValueDisabled("string")).toBe(false);
    const params = toTypedRequestParameters({
      sql: "select :x",
      params: [{ name: "x", type: "null", value: "ignored" }],
    });
    expect(params[0].value).toBe("");
  });
});
