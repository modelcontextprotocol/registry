// Verifies that docs/reference/api/openapi.yaml references the generated
// server.json schema instead of duplicating it.
//
// schema.ts is the single source of truth: `npm run generate` produces
// draft/server.schema.json, and openapi.yaml's components/schemas exposes each
// server.json definition as a thin external $ref into that file. This check
// fails (exit 1) if any definition is missing its stub, a stub points at a
// definition that does not exist, or a stub's shape drifts -- e.g. after adding
// or renaming a top-level definition in schema.ts.

import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { parse } from "yaml";

import { schema } from "../schema.ts";

const __dirname = dirname(fileURLToPath(import.meta.url));
const OPENAPI = join(__dirname, "..", "..", "api", "openapi.yaml");
const SCHEMA_REF_PREFIX = "../server-json/draft/server.schema.json#/definitions/";

type Schemas = Record<string, unknown>;

function expectedStub(name: string): { $ref: string } {
  return { $ref: `${SCHEMA_REF_PREFIX}${name}` };
}

function main(): void {
  const definitions = (schema.definitions ?? {}) as Schemas;
  const definitionNames = Object.keys(definitions);

  const openapi = parse(readFileSync(OPENAPI, "utf-8")) as {
    components?: { schemas?: Schemas };
  };
  const schemas = openapi.components?.schemas;
  if (!schemas) {
    console.error("✗ openapi.yaml has no components.schemas");
    process.exit(1);
  }

  const errors: string[] = [];

  // Every server.json definition must be exposed as the expected external $ref.
  for (const name of definitionNames) {
    const actual = schemas[name];
    const expected = expectedStub(name);
    if (JSON.stringify(actual) !== JSON.stringify(expected)) {
      errors.push(
        `components.schemas.${name} should be ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`,
      );
    }
  }

  // Every external $ref into server.schema.json must target a real definition.
  for (const [name, value] of Object.entries(schemas)) {
    if (
      value &&
      typeof value === "object" &&
      "$ref" in value &&
      typeof (value as { $ref: unknown }).$ref === "string"
    ) {
      const ref = (value as { $ref: string }).$ref;
      if (ref.startsWith(SCHEMA_REF_PREFIX)) {
        const target = ref.slice(SCHEMA_REF_PREFIX.length);
        if (!(target in definitions)) {
          errors.push(
            `components.schemas.${name} references unknown definition '${target}'`,
          );
        }
      }
    }
  }

  if (errors.length > 0) {
    console.error("✗ openapi.yaml is out of sync with schema.ts:");
    for (const e of errors) console.error(`  - ${e}`);
    console.error("\nUpdate the external $ref stubs in openapi.yaml's components/schemas.");
    process.exit(1);
  }

  console.log(
    `✓ openapi.yaml references all ${definitionNames.length} server.json definitions via external $ref`,
  );
}

main();
