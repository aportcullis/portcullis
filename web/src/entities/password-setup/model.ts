import { Code, ConnectError } from "@connectrpc/connect";
import { passwordSetupToken } from "@/shared/lib/passwordSetupLink";

/** Identifies the current stage of a password setup interaction. */
export type SetupStatus = "ready" | "busy" | "succeeded" | "refused" | "uncertain";

export type SetupState = { status: SetupStatus; message: string };
export type SetupFlow = { state: () => SetupState; submit: (password: string, confirmation: string) => Promise<void>; allowRetry: () => void; dispose: () => void };

/** Owns a volatile bearer credential and fences completion after disposal. */
export function createPasswordSetupFlow(initialToken: string, complete: (token: string, password: string) => Promise<unknown>, changed: (state: SetupState) => void): SetupFlow {
  let token = passwordSetupToken(`#token=${initialToken}`);
  let active = true;
  let state: SetupState = token ? { status: "ready", message: "" } : { status: "refused", message: "Invalid or expired setup link. Ask your administrator for a new link." };
  const update = (next: SetupState) => { state = next; if (active) changed(next); };
  return {
    state: () => state,
    async submit(password, confirmation) {
      if (!active || state.status !== "ready") return;
      const normalized = password.normalize("NFC");
      if (normalized !== confirmation.normalize("NFC")) { update({ status: "ready", message: "Passwords must match." }); return; }
      if ([...normalized].length < 15 || [...normalized].length > 1024) { update({ status: "ready", message: "Use 15 to 1024 characters." }); return; }
      update({ status: "busy", message: "" });
      try {
        await complete(token, normalized);
        if (!active) return;
        token = ""; update({ status: "succeeded", message: "Password set. Sign in with your new password." });
      } catch (error) {
        if (!active) return;
        if (error instanceof ConnectError && error.code === Code.PermissionDenied) {
          token = ""; update({ status: "refused", message: "Invalid or expired setup link. Ask your administrator for a new link." });
        } else if (error instanceof ConnectError && error.code === Code.ResourceExhausted) {
          update({ status: "ready", message: "Temporarily busy. Wait a moment and try again." });
        } else if (error instanceof ConnectError && error.code === Code.InvalidArgument) {
          update({ status: "ready", message: "Password does not meet policy. Use 15 to 1024 characters." });
        } else {
          update({ status: "uncertain", message: "The outcome could not be confirmed. Try signing in, or explicitly retry setup. No request was retried automatically." });
        }
      }
    },
    allowRetry() { if (active && state.status === "uncertain") update({ status: "ready", message: "Enter and confirm your password to retry." }); },
    dispose() { active = false; token = ""; },
  };
}
