import { describe, expect, it } from "vitest";

import { Code, ConnectError } from "@connectrpc/connect";

import { describeBootstrapError } from "@/features/auth/bootstrapError";

const setupTokenGuidance =
  "The setup token is missing, wrong, expired or already used. Copy the latest token from the server log or setup token file; restarting the server issues a new one.";

describe("bootstrap error guidance", () => {
  it("explains how to obtain a valid setup token when the server refuses it", () => {
    expect(describeBootstrapError(new ConnectError("invalid setup token", Code.PermissionDenied))).toBe(setupTokenGuidance);
  });

  it("keeps the existing guidance for setup, validation and rate-limit refusals", () => {
    expect(describeBootstrapError(new ConnectError("already bootstrapped", Code.FailedPrecondition))).toBe("This instance is already set up.");
    expect(describeBootstrapError(new ConnectError("invalid email", Code.InvalidArgument))).toContain("15 characters");
    expect(describeBootstrapError(new ConnectError("slow down", Code.ResourceExhausted))).toBe("Too many attempts — wait a moment and retry.");
  });

  it("does not present an unknown, network or server failure as a token problem", () => {
    expect(describeBootstrapError(new ConnectError("unexpected", Code.Unknown))).toBe("Something went wrong. Please retry.");
    expect(describeBootstrapError(new Error("network down"))).toBe("Something went wrong. Please retry.");
    expect(describeBootstrapError(new ConnectError("internal error", Code.Internal))).not.toBe(setupTokenGuidance);
  });
});
