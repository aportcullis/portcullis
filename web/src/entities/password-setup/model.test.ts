import { describe, expect, it } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import { createPasswordSetupFlow } from "@/entities/password-setup/model";

const token = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA";
const password = "correct-horse-battery";

describe("initial password setup lifetime", () => {
  it("keeps a throttled credential for an explicit retry without claiming an uncertain commit", async () => {
    let calls = 0;
    const flow = createPasswordSetupFlow(token, async () => { calls++; throw new ConnectError("temporarily busy", Code.ResourceExhausted); }, () => {});
    await flow.submit(password, password);
    expect(flow.state().status).toBe("ready"); expect(flow.state().message).toContain("busy"); expect(calls).toBe(1);
  });
  it.each([password, "e\u0301quivalent-long-password", "a long spaced password  ", "another valid password"])("completes a confirmed password without replay: %s", async input => {
    const calls: string[][] = [];
    const flow = createPasswordSetupFlow(token, async (secret, value) => { calls.push([secret, value]); }, () => {});
    await flow.submit(input, input);
    expect(flow.state().status).toBe("succeeded");
    await flow.submit(input, input);
    expect(calls).toEqual([[token, input.normalize("NFC")]]);
  });
  it.each(["expired", "reused", "revoked", "disabled"])("discards a refused %s token without retry", async () => {
    let calls = 0;
    const flow = createPasswordSetupFlow(token, async () => { calls++; throw new ConnectError("invalid or expired password setup link", Code.PermissionDenied); }, () => {});
    await flow.submit(password, password);
    expect(flow.state().status).toBe("refused");
    flow.allowRetry(); await flow.submit(password, password);
    expect(calls).toBe(1);
  });
  it("requires confirmation and the existing password length", async () => {
    let calls = 0; const flow = createPasswordSetupFlow(token, async () => { calls++; }, () => {});
    await flow.submit(password, "different"); expect(flow.state().message).toContain("match");
    await flow.submit("short", "short"); expect(flow.state().message).toContain("15");
    expect(calls).toBe(0);
  });
  it("fences duplicate submissions and responses after route disposal", async () => {
    let release = () => {}; let calls = 0; let changes = 0;
    const held = new Promise<void>(resolve => { release = resolve; });
    const flow = createPasswordSetupFlow(token, async () => { calls++; await held; }, () => { changes++; });
    const pending = flow.submit(password, password); await flow.submit(password, password);
    flow.dispose(); const before = changes; release(); await pending;
    expect(calls).toBe(1); expect(changes).toBe(before);
  });
  it("requires an explicit choice before retrying an uncertain write", async () => {
    let calls = 0; const flow = createPasswordSetupFlow(token, async () => { calls++; if (calls === 1) throw new Error("lost response"); }, () => {});
    await flow.submit(password, password); expect(flow.state().status).toBe("uncertain");
    await flow.submit(password, password); expect(calls).toBe(1);
    flow.allowRetry(); await flow.submit(password, password); expect(flow.state().status).toBe("succeeded");
  });
});
