import SwaggerParser from "@apidevtools/swagger-parser";
import openapiTS, { astToString } from "openapi-typescript";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const check = process.argv[2] === "--check";
try {
  for (const service of ["catalog-api", "user-api"]) {
    const spec = resolve(root, "contracts", `${service}.openapi.json`);
    await SwaggerParser.validate(spec);
    const ast = await openapiTS(new URL(`../contracts/${service}.openapi.json`, import.meta.url));
    const source = `// Generated from contracts/${service}.openapi.json. Run vp run contracts:generate.\n${astToString(ast)}`;
    const target = resolve(root, "apps/web/server/clients/generated", `${service}.d.ts`);
    if (check) {
      if ((await readFile(target, "utf8")) !== source)
        throw new Error(`${service} generated types are stale. Run vp run contracts:generate.`);
    } else {
      await mkdir(dirname(target), { recursive: true });
      await writeFile(target, source);
    }
    console.log(`${service}: OpenAPI validated; generated types ${check ? "match" : "written"}.`);
  }
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
