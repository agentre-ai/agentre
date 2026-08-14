import { realpathSync } from "node:fs";
import { join } from "node:path";

import { expect, test, type Page } from "@playwright/test";

import {
  assistantMessageCountContaining,
  e2eDataDir,
  errorSessionCountContaining,
  mainDatabaseFileFromPragma,
  runningSessionCount,
  userMessageCountContaining,
} from "../fixtures/db";

const runID = process.env.AGENTRE_E2E_RUN_ID;
if (!runID) throw new Error("desktop smoke must be started by the E2E runner");
const SUCCESS_PROMPT = `desktop-smoke-persist-${runID}`;
const FAILURE_REASON = `desktop-smoke-boundary-${runID}`;
const FAILURE_PROMPT = `e2e-runtime-fail:${FAILURE_REASON}`;
const FAILURE_TEXT = `e2e-runtime-failure: ${FAILURE_REASON}`;

async function createChat(page: Page) {
  await page.getByTestId("new-chat-button").click();
  await page.getByTestId("new-agent-chat-item").click();
  await page.locator('[data-testid^="agent-picker-item-"]').first().click();
  await expect(page.locator('[role="tab"][data-active="true"]')).toBeVisible();
}

async function send(page: Page, text: string) {
  const editor = page.locator(".ProseMirror");
  await expect(editor).toBeVisible();
  await editor.click();
  await editor.pressSequentially(text);
  await page.getByRole("main").locator('button[type="submit"]').click();
}

test.describe.serial("desktop smoke", () => {
  test("Given a fresh isolated app, when chat completes and the page reloads, then UI and SQLite retain the deterministic turn", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByTestId("new-chat-button")).toBeVisible();
    await createChat(page);
    await send(page, SUCCESS_PROMPT);

    await expect(page.getByText(`e2e-fake-reply: ${SUCCESS_PROMPT}`)).toBeVisible();
    await expect(page.getByTestId("tab-spinner")).toHaveCount(0);
    await expect.poll(() => runningSessionCount()).toBe(0);
    expect(userMessageCountContaining(SUCCESS_PROMPT)).toBe(1);
    expect(assistantMessageCountContaining(`e2e-fake-reply: ${SUCCESS_PROMPT}`)).toBe(1);
    expect(mainDatabaseFileFromPragma()).toBe(realpathSync(join(e2eDataDir(), "agentre.db")));

    await page.reload();
    await expect(page.getByText(SUCCESS_PROMPT, { exact: true }).first()).toBeVisible();
    await expect(page.getByText(`e2e-fake-reply: ${SUCCESS_PROMPT}`)).toBeVisible();
    await expect(page.getByTestId("tab-spinner")).toHaveCount(0);
  });

  test("Given the deterministic runtime failure directive, when a turn fails, then the UI and SQLite reach an error terminal state instead of running forever", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByTestId("new-chat-button")).toBeVisible();
    await createChat(page);
    await send(page, FAILURE_PROMPT);

    await expect(page.getByText(new RegExp(`Agent call failed: ${FAILURE_TEXT}`))).toBeVisible();
    await expect(page.getByTestId("tab-spinner")).toHaveCount(0);
    await expect.poll(() => runningSessionCount()).toBe(0);
    await expect.poll(() => errorSessionCountContaining(FAILURE_TEXT)).toBe(1);
  });
});
