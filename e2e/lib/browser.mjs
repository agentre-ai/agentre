export const VERIFY_VIEWPORT = Object.freeze({ width: 1440, height: 900 });

export async function applyVerificationViewport(page) {
  await page.setViewportSize(VERIFY_VIEWPORT);
}

export function verificationBrowserArgs({ cdpPort, browserDir, headless }) {
  const args = [
    `--remote-debugging-port=${cdpPort}`,
    `--user-data-dir=${browserDir}`,
    `--window-size=${VERIFY_VIEWPORT.width},${VERIFY_VIEWPORT.height}`,
    "--no-first-run",
    "--no-default-browser-check",
    "--no-service-autorun",
    "--password-store=basic",
    "--use-mock-keychain",
  ];
  if (headless) args.push("--headless=new");
  return args;
}
