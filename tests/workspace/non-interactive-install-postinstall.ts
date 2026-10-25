import assert from "node:assert/strict";
import { writeFileSync } from "node:fs";
import { isAbsolute } from "node:path";

/** Records a forbidden lifecycle execution only in the fixture's supplied marker file. */
function recordInstallScript(): void {
  const { PORTCULLIS_SCRIPT_MARKER: marker } = process.env;
  assert.ok(typeof marker === "string" && isAbsolute(marker), "The owned lifecycle marker must be an absolute path");
  writeFileSync(marker, "ran");
}

recordInstallScript();
