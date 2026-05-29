import path from "node:path";
import { ESLint } from "eslint";
import { describe, expect, it } from "vitest";

async function lintFixture(code: string) {
  const eslint = new ESLint({ cwd: process.cwd() });
  const [result] = await eslint.lintText(code, {
    filePath: path.join(
      process.cwd(),
      "src/components/agentre/i18n-lint-fixture.tsx",
    ),
  });

  return result.messages.map((message) => message.ruleId);
}

describe("ESLint i18n rules", () => {
  it("Given Chinese JSX UI text, When ESLint runs, Then it reports the hard-coded text", async () => {
    await expect(
      lintFixture(
        "export function Fixture() { return <button>新增硬编码文案</button>; }",
      ),
    ).resolves.toContain("i18next/no-literal-string");
  });

  it("Given Chinese accessible attribute text, When ESLint runs, Then it reports the hard-coded text", async () => {
    await expect(
      lintFixture(
        'export function Fixture() { return <button aria-label="新增硬编码文案" />; }',
      ),
    ).resolves.toContain("i18next/no-literal-string");
  });

  it("Given legacy Chinese JSX text, When ESLint runs, Then it reports the hard-coded text instead of using an allowlist", async () => {
    await expect(
      lintFixture("export function Fixture() { return <span>名称</span>; }"),
    ).resolves.toContain("i18next/no-literal-string");
  });

  it("Given translated JSX text, When ESLint runs, Then it accepts the translation call", async () => {
    await expect(
      lintFixture(
        'import { useTranslation } from "react-i18next"; export function Fixture() { const { t } = useTranslation(); return <button>{t("nav.chat")}</button>; }',
      ),
    ).resolves.not.toContain("i18next/no-literal-string");
  });

  it("Given dynamic output rendered as an expression, When ESLint runs, Then it does not report the expression", async () => {
    await expect(
      lintFixture(
        "export function Fixture({ output }: { output: string }) { return <pre>{output}</pre>; }",
      ),
    ).resolves.not.toContain("i18next/no-literal-string");
  });

  it("Given code sample text, When ESLint runs, Then it keeps non-UI code content passing", async () => {
    await expect(
      lintFixture(
        "export function Fixture() { return <code>新增硬编码文案</code>; }",
      ),
    ).resolves.not.toContain("i18next/no-literal-string");
  });
});
