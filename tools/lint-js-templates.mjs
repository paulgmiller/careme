#!/usr/bin/env node
import { readFileSync } from "node:fs";
import vm from "node:vm";

const templatePaths = process.argv.slice(2);
if (templatePaths.length === 0) {
  templatePaths.push("internal/templates/user_clerk_billing.html");
}

let checked = 0;
for (const templatePath of templatePaths) {
  const template = readFileSync(templatePath, "utf8");
  const scripts = template.matchAll(/<script\b(?=[^>]*\bdata-js-lint=)[^>]*>([\s\S]*?)<\/script>/gi);

  for (const [match, source] of scripts) {
    checked++;
    const line = template.slice(0, match.index).split("\n").length;
    try {
      new vm.Script(source, { filename: `${templatePath}:${line}` });
    } catch (error) {
      console.error(`${templatePath}:${line}: JavaScript syntax check failed`);
      console.error(error.message);
      process.exitCode = 1;
    }
  }
}

if (checked === 0) {
  console.error("No template scripts marked with data-js-lint were found.");
  process.exit(1);
}

if (!process.exitCode) {
  console.log(`Checked ${checked} template JavaScript block(s).`);
}
