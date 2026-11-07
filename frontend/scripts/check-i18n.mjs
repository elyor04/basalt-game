// Lists dictionary keys that no source file references (directly or via a
// known runtime prefix such as `level.${id}`).
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
const en = readFileSync(new URL("../src/i18n/en.ts", import.meta.url), "utf8");
const keys = [...en.matchAll(/^\s+"([a-z0-9_.]+)":/gim)].map((m) => m[1]);
const files = [];
const walk = (d) => readdirSync(d).forEach((f) => { const p = join(d, f); statSync(p).isDirectory() ? walk(p) : /\.tsx?$/.test(f) && !p.includes("i18n") && files.push(p); });
walk(new URL("../src", import.meta.url).pathname.replace(/^\/([A-Z]:)/, "$1"));
const src = files.map((f) => readFileSync(f, "utf8")).join("\n");
const dynamic = ["level.", "ach.", "error.", "player."];
const unused = keys.filter((k) => !src.includes(`"${k}"`) && !dynamic.some((p) => k.startsWith(p)));
console.log(unused.length ? `unused keys:\n  ${unused.join("\n  ")}` : `ok: all ${keys.length} keys referenced`);
