import { describe, expect, it, vi } from "vitest";


type TransportOptions = { baseUrl: string; useBinaryFormat?: boolean };
const createConnectTransport = vi.hoisted(() =>
  vi.fn((options: TransportOptions) => ({ kind: "transport", baseUrl: options.baseUrl })),
);
vi.mock("@connectrpc/connect-web", () => ({ createConnectTransport }));
vi.mock("@connectrpc/connect", () => ({
  createClient: vi.fn(() => ({})),
  Code: {},
  ConnectError: class {},
}));

describe("api transport", () => {
  it("uses the binary format so payload sizes match what the server budgets", async () => {
    await import("@/shared/api/client");

    expect(createConnectTransport).toHaveBeenCalledTimes(1);
    const options: unknown = createConnectTransport.mock.calls[0]?.[0];
    expect(options).toMatchObject({ useBinaryFormat: true });
  });
});
