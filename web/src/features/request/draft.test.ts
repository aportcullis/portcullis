import { create } from "@bufbuild/protobuf";
import { AccessRequestPayloadSchema } from "@/gen/portcullis/v1/access_requests_pb";
import { describe, expect, it } from "vitest";

import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import {
  appendDraftParameter,
  createEmptyRequestDraft,
  createRequestDraftFromPayload,
  resolveUnavailableTargetRequestState,
  isSelectedTargetAvailable,
  resolveUnavailableTargetAction,
  removeDraftParameter,
  updateDraftParameter,
  toTypedRequestParameters,
  validateRequestDraft,
  isParameterValueDisabled,
  parseParameterType,
} from "@/features/request/draft";

describe("request draft", () => {
  it("adds, patches, and removes parameter rows immutably", () => {
    const appended = appendDraftParameter(createEmptyRequestDraft());
    expect(appended.params).toHaveLength(1);
    const patched = updateDraftParameter(appended, 0, { name: "id", type: "integer", value: "7" });
    expect(appended.params[0].name).toBe("");
    expect(patched.params[0]).toEqual({ name: "id", type: "integer", value: "7" });
    const removed = removeDraftParameter(patched, 0);
    expect(removed.params).toHaveLength(0);
  });

  it.each(["string", "integer", "null", "uuid", "timestamp"])("parses the select value %s into a parameter type", (value) => {
    expect(parseParameterType(value)).toBe(value);
  });

  it.each(["", "INTEGER", "int", " string", "__proto__", "toString"])("refuses the unsupported select value %j", (value) => {
    expect(parseParameterType(value)).toBeUndefined();
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
    expect(validateRequestDraft({ title: "Query review", body: "", sql: "   ", params: [] })).toMatch(/SQL/);
    expect(validateRequestDraft({ title: "Query review", body: "", sql: "select 1", params: [] })).toBe("");
  });

  it("rejects unnamed and duplicate parameters", () => {
    expect(validateRequestDraft({ title: "Query review", body: "", sql: "select :a", params: [{ name: "", type: "string", value: "x" }] })).toMatch(
      /needs a name/,
    );
    expect(
      validateRequestDraft({
        title: "Query review", body: "", sql: "select :a",
        params: [
          { name: "a", type: "string", value: "1" },
          { name: "a", type: "string", value: "2" },
        ],
      }),
    ).toMatch(/more than once/);
  });

  it("validates typed values", () => {
    expect(validateRequestDraft({ title: "Query review", body: "", sql: "s", params: [{ name: "n", type: "integer", value: "seven" }] })).toMatch(
      /whole number/,
    );
    expect(validateRequestDraft({ title: "Query review", body: "", sql: "s", params: [{ name: "n", type: "integer", value: "7" }] })).toBe("");
    expect(validateRequestDraft({ title: "Query review", body: "", sql: "s", params: [{ name: "b", type: "boolean", value: "yes" }] })).toMatch(
      /true or false/,
    );
    expect(validateRequestDraft({ title: "Query review", body: "", sql: "s", params: [{ name: "u", type: "uuid", value: "not-a-uuid" }] })).toMatch(
      /UUID/,
    );
  });

  it("treats null parameters as valueless", () => {
    expect(isParameterValueDisabled("null")).toBe(true);
    expect(isParameterValueDisabled("string")).toBe(false);
    const params = toTypedRequestParameters({
      title: "Query review", body: "", sql: "select :x",
      params: [{ name: "x", type: "null", value: "ignored" }],
    });
    expect(params[0].value).toBe("");
  });
});

describe("request narrative", () => {
  it("reopens a saved narrative without losing literal text or parameter edits", () => {
    const payload = create(AccessRequestPayloadSchema, { title: "Revenue", body: "Purpose\n<script>literal</script>", sql: "select 1" });
    const draft = createRequestDraftFromPayload(payload);
    expect(draft.title).toBe("Revenue");
    expect(draft.body).toBe(payload.body);
    const edited = appendDraftParameter(draft);
    expect(edited.title).toBe("Revenue");
    expect(edited.body).toBe(payload.body);
  });
  it("requires a title and counts narrative limits as Unicode code points", () => {
    const draft = { title: "", body: "", sql: "select 1", params: [] };
    expect(validateRequestDraft(draft)).toMatch(/title/i);
    expect(validateRequestDraft({ ...draft, title: "😀".repeat(200), body: "😀".repeat(4000) }, { maxRequestTitleChars: 200, maxRequestBodyChars: 4000 })).toBe("");
    expect(validateRequestDraft({ ...draft, title: "😀".repeat(201) }, { maxRequestTitleChars: 200, maxRequestBodyChars: 4000 })).toMatch(/title/i);
    expect(validateRequestDraft({ ...draft, title: "Title", body: "😀".repeat(4001) }, { maxRequestTitleChars: 200, maxRequestBodyChars: 4000 })).toMatch(/body/i);
  });
});
