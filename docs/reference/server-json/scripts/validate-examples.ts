// Validates the example documents under examples/ against the generated
// draft/server.schema.json. Each file under examples/valid must validate
// cleanly; each file under examples/invalid must fail at least one constraint.
//
// Mirrors the experimental Server Card repo's validate-examples.ts. The schema
// is draft-07 (root $ref -> #/definitions/ServerDetail), so it is compiled with
// the default Ajv entrypoint.

import { readdirSync, readFileSync } from "node:fs";
import { basename, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import Ajv from "ajv";
import addFormats from "ajv-formats";

const __dirname = dirname(fileURLToPath(import.meta.url));
const SCHEMA_JSON = join(__dirname, "..", "draft", "server.schema.json");
const EXAMPLES_DIR = join(__dirname, "..", "examples");

type Outcome = {
  name: string;
  passed: boolean;
  message: string;
};

function listJsonFiles(dir: string): string[] {
  try {
    return readdirSync(dir)
      .filter((f) => f.endsWith(".json"))
      .sort()
      .map((f) => join(dir, f));
  } catch {
    return [];
  }
}

function main(): void {
  const schema = JSON.parse(readFileSync(SCHEMA_JSON, "utf-8"));
  const ajv = new Ajv({ allErrors: true, strict: false });
  addFormats(ajv);
  const validate = ajv.compile(schema);

  const outcomes: Outcome[] = [];
  for (const expected of ["valid", "invalid"] as const) {
    for (const file of listJsonFiles(join(EXAMPLES_DIR, expected))) {
      const data = JSON.parse(readFileSync(file, "utf-8"));
      const ok = validate(data);
      const passed = expected === "valid" ? ok : !ok;
      const errors = validate.errors ?? [];
      outcomes.push({
        name: `${expected}/${basename(file)}`,
        passed,
        message: passed
          ? expected === "valid"
            ? "validated cleanly"
            : `rejected (${errors.length} error(s))`
          : expected === "valid"
            ? `unexpectedly invalid: ${ajv.errorsText(errors)}`
            : "unexpectedly valid (no errors raised)",
      });
    }
  }

  if (outcomes.length === 0) {
    console.error(`No examples found under ${EXAMPLES_DIR}/.`);
    process.exit(1);
  }

  let failed = 0;
  for (const o of outcomes) {
    console.log(`${o.passed ? "✓" : "✗"} ${o.name} — ${o.message}`);
    if (!o.passed) failed++;
  }
  console.log();
  if (failed > 0) {
    console.error(`${failed} of ${outcomes.length} example(s) failed.`);
    process.exit(1);
  }
  console.log(`All ${outcomes.length} example(s) passed.`);
}

main();
