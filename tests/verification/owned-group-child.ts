import assert from "node:assert/strict";
import { fork } from "node:child_process";
import { fileURLToPath } from "node:url";
import { MessageChannel } from "node:worker_threads";

const [role, mode] = process.argv.slice(2);
assert.ok(role === "leader" || role === "descendant", "The fixture role must be explicit");
assert.ok(mode === "leader-only" || mode === "leader-descendant", "The fixture membership must be explicit");
assert.ok(typeof process.send === "function", "The fixture requires its owned IPC channel");

// A referenced port retains the descendant after its leader exits, without timed ordering.
const lifetime = new MessageChannel();
lifetime.port1.start();
lifetime.port1.ref();

/** Sends only fixture identity through the established native IPC channel. */
function sendFixtureMessage(message: object): void {
  assert.ok(typeof process.send === "function", "The fixture IPC channel must remain available");
  process.send(message, (error: Error | null) => {
    if (error !== null) throw error;
  });
}

/** Recognizes the sole controlled leader-release command. */
function isLeaderRelease(value: unknown): boolean {
  return typeof value === "object" && value !== null && "kind" in value && value.kind === "release-leader";
}

if (role === "descendant") {
  sendFixtureMessage({ kind: "descendant-ready", process: process.pid, parent: process.ppid });
} else {
  process.on("message", (value: unknown) => {
    assert.ok(isLeaderRelease(value), "Only the owner may release the fixture leader");
    lifetime.port1.close();
    lifetime.port2.close();
    process.exit(0);
  });
  if (mode === "leader-only") {
    sendFixtureMessage({ kind: "group-ready", leader: process.pid, descendant: { kind: "absent" } });
  } else {
    const descendant = fork(fileURLToPath(import.meta.url), ["descendant", mode], {
      execArgv: [],
      detached: false,
      stdio: ["ignore", "inherit", "inherit", "ipc"],
    });
    const descendantIdentifier = descendant.pid;
    assert.ok(typeof descendantIdentifier === "number" && descendantIdentifier > 1, "The owned descendant must start");
    descendant.on("error", (error: Error) => { throw error; });
    descendant.on("message", (value: unknown) => {
      assert.ok(typeof value === "object" && value !== null && "kind" in value && value.kind === "descendant-ready"
        && "process" in value && value.process === descendantIdentifier && "parent" in value && value.parent === process.pid,
      "The ready message must identify the owned descendant");
      sendFixtureMessage({ kind: "group-ready", leader: process.pid, descendant: { kind: "present", process: descendantIdentifier, parent: process.pid } });
    });
  }
}
