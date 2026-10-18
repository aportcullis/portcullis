import { expect, it } from "vitest";
import { passwordSetupLink, passwordSetupToken } from "@/shared/lib/passwordSetupLink";

const token = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA";
it("delivers only a fragment on the caller's own origin", () => {
 const link = new URL(passwordSetupLink("https://app.example", token));
 expect(link.origin).toBe("https://app.example"); expect(link.pathname).toBe("/setup-password"); expect(link.search).toBe(""); expect(passwordSetupToken(link.hash)).toBe(token);
});
it.each(["", "#token=bad", `#token=${token}&token=${token}`, `?token=${token}`])("refuses ambiguous or nonfragment credentials: %s", input => { expect(passwordSetupToken(input)).toBe(""); });
