import { FULL_GUARD_TESTS, runNodeGuards } from "./lib/guard-suite.mjs";

runNodeGuards({ tests: FULL_GUARD_TESTS }).catch((error) => {
  console.error(error.message);
  process.exit(1);
});
