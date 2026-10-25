import { assert, describe, expect, it, vi } from "vitest";
import type { ConnectTransportOptions } from "@connectrpc/connect-web";

const createConnectTransport = vi.hoisted(() =>
  vi.fn((options: ConnectTransportOptions) => ({ kind: "transport", baseUrl: options.baseUrl })),
);
vi.mock("@connectrpc/connect-web", () => ({ createConnectTransport }));
vi.mock("@connectrpc/connect", async importOriginal => {
  const implementation = await importOriginal<typeof import("@connectrpc/connect")>();
  return { ...implementation, createClient: vi.fn(() => ({})) };
});

describe("api transport", () => {
  it("uses the binary format so payload sizes match what the server budgets", async () => {
    await import("@/shared/api/client");

    expect(createConnectTransport).toHaveBeenCalledTimes(1);
    const call = createConnectTransport.mock.calls[0];
    assert.isDefined(call, "Transport options were not captured");
    const [options] = call;
    expect(options).toMatchObject({ useBinaryFormat: true });
  });
});
