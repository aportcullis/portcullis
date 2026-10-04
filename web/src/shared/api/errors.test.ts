import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";

import { errorMessage } from "@/shared/api/errors";

describe("errorMessage", () => {
  it("shows a Connect error's server message without the code prefix", () => {
    expect(errorMessage(new ConnectError("connection is archived", Code.FailedPrecondition))).toBe("connection is archived");
  });

  it("shows a permission refusal's message", () => {
    expect(errorMessage(new ConnectError("permission denied", Code.PermissionDenied))).toBe("permission denied");
  });

  it("keeps an empty server message empty rather than inventing one", () => {
    expect(errorMessage(new ConnectError("", Code.Internal))).toBe("");
  });

  it("hides a plain Error's message, which may carry client detail", () => {
    expect(errorMessage(new Error("select * from secrets"))).toBe("Request failed.");
  });

  it("hides a browser network failure", () => {
    expect(errorMessage(new TypeError("Failed to fetch"))).toBe("Request failed.");
  });

  it("hides a thrown string", () => {
    expect(errorMessage("raw text")).toBe("Request failed.");
  });

  it("hides an object that only looks like a Connect error", () => {
    expect(errorMessage({ rawMessage: "forged", code: Code.Internal })).toBe("Request failed.");
  });
});
