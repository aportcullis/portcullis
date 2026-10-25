import assert from "node:assert/strict";
import { existsSync, writeFileSync } from "node:fs";
import { join } from "node:path";

function readProgressChildConfiguration(): { directory: string; behavior: "pass" | "fail"; hold: boolean } {
  const { VERIFICATION_PROGRESS_DIRECTORY: directory, VERIFICATION_PROGRESS_BEHAVIOR: behavior, VERIFICATION_PROGRESS_HOLD: hold } = process.env;
  assert.ok(typeof directory === "string" && directory !== "");
  assert.ok(behavior === "pass" || behavior === "fail");
  assert.ok(hold === "true" || hold === "false");
  return { directory, behavior, hold: hold === "true" };
}

const { directory, behavior, hold } = readProgressChildConfiguration();

type ProgressCompletion = { kind: "released" } | { kind: "cancelled"; signal: "SIGINT" | "SIGTERM" };

async function awaitProgressCompletion(): Promise<ProgressCompletion> {
  if (!hold) return { kind: "released" };
  return new Promise(resolve => {
    const complete = (completion: ProgressCompletion): void => {
      clearInterval(timer);
      process.removeListener("SIGINT", interrupt);
      process.removeListener("SIGTERM", terminate);
      resolve(completion);
    };
    const interrupt = (): void => complete({ kind: "cancelled", signal: "SIGINT" });
    const terminate = (): void => complete({ kind: "cancelled", signal: "SIGTERM" });
    const timer = setInterval(() => { if (existsSync(join(directory, "release"))) complete({ kind: "released" }); }, 10);
    process.once("SIGINT", interrupt);
    process.once("SIGTERM", terminate);
  });
}

process.stdout.write("fixture stdout before completion\n");
process.stderr.write("fixture stderr before completion\n");
writeFileSync(join(directory, "ready"), JSON.stringify({ process: process.pid }));
const completion = await awaitProgressCompletion();
if (completion.kind === "cancelled") {
  writeFileSync(join(directory, "cancelled"), completion.signal);
  process.exitCode = completion.signal === "SIGINT" ? 130 : 143;
} else {
  process.stdout.write("fixture completed\n");
  process.exitCode = behavior === "pass" ? 0 : 7;
}
