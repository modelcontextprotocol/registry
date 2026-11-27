// Generates draft/server.schema.json from schema.ts.
//
// With --check, regenerates in memory and fails (exit 1) if the committed file
// is out of date instead of writing it. Mirrors the experimental Server Card
// repo's generate-schema.ts (TS source of truth -> generated JSON Schema).
//
// The serialization deliberately reproduces Go's encoding/json output
// (MarshalIndent with a two-space indent) so this change is a strict no-op
// against the schema previously produced by tools/extract-server-schema:
//   - object keys are sorted ascending (Go sorts map keys); array order is kept
//   - <, >, & and U+2028/U+2029 are escaped (Go's SetEscapeHTML default)
//   - a trailing newline is appended

import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { schema } from "../schema.ts";

const __dirname = dirname(fileURLToPath(import.meta.url));
const SCHEMA_JSON = join(__dirname, "..", "draft", "server.schema.json");

const CHECK_MODE = process.argv.includes("--check");

function sortKeys(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sortKeys);
  if (value && typeof value === "object") {
    const sorted: Record<string, unknown> = {};
    for (const key of Object.keys(value as Record<string, unknown>).sort()) {
      sorted[key] = sortKeys((value as Record<string, unknown>)[key]);
    }
    return sorted;
  }
  return value;
}

// Match Go's encoding/json HTML escaping so output is byte-for-byte identical.
function escapeLikeGo(json: string): string {
  return json
    .replace(/</g, "\\u003c")
    .replace(/>/g, "\\u003e")
    .replace(/&/g, "\\u0026")
    .replace(new RegExp("\u2028", "g"), "\\u2028")
    .replace(new RegExp("\u2029", "g"), "\\u2029");
}

function render(): string {
  return escapeLikeGo(JSON.stringify(sortKeys(schema), null, 2)) + "\n";
}

function main(): void {
  const expected = render();
  if (CHECK_MODE) {
    const existing = readFileSync(SCHEMA_JSON, "utf-8");
    if (existing !== expected) {
      console.error(
        "✗ draft/server.schema.json is out of date. Run: npm run generate",
      );
      process.exit(1);
    }
    console.log("✓ draft/server.schema.json is up to date");
    return;
  }
  writeFileSync(SCHEMA_JSON, expected, "utf-8");
  console.log("✓ Generated draft/server.schema.json");
}

main();
