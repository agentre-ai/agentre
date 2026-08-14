import { runNodeGuards } from "./lib/guard-suite.mjs";

runNodeGuards().catch((error) => {
  console.error(error.message);
  process.exit(1);
});
