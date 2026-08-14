import { useState, type ReactNode } from "react";
import { ArrowRight, Check, X } from "lucide-react";
import { Trans, useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";

import { CommandCard } from "./command-card";
import { DevicePairingForm, type AddRequest } from "./device-pairing-form";

type RemoteOS = "linux" | "macos" | "windows";
type RunMode = "background" | "foreground";
type OnboardingStep = 1 | 2 | 3;

type AgentredOnboardingProps = {
  /** 省略即不可收起 —— 宿主没有可回退的地方(设备列表为空)时就是这种情况。 */
  onDismiss?: () => void;
  onSubmit: (request: AddRequest) => Promise<void>;
};

const MANUAL_INSTALL_URL =
  "https://github.com/agentre-ai/agentre/releases/latest";

export function AgentredOnboarding({
  onDismiss,
  onSubmit,
}: AgentredOnboardingProps) {
  const { t } = useTranslation();
  const [step, setStep] = useState<OnboardingStep>(1);
  const [remoteOS, setRemoteOS] = useState<RemoteOS>("linux");
  const [runMode, setRunMode] = useState<RunMode>("background");

  const commands = {
    daemonRunning: t("remoteDevices.onboarding.commands.daemonRunning"),
    foregroundRun: t("remoteDevices.onboarding.commands.foregroundRun"),
    installUnix: t("remoteDevices.onboarding.commands.installUnix"),
    installWindows: t("remoteDevices.onboarding.commands.installWindows"),
    listenAddress: t("remoteDevices.onboarding.commands.listenAddress"),
    pair: t("remoteDevices.onboarding.commands.pair"),
    serviceInstall: t("remoteDevices.onboarding.commands.serviceInstall"),
    serviceRestart: t("remoteDevices.onboarding.commands.serviceRestart"),
    serviceStatus: t("remoteDevices.onboarding.commands.serviceStatus"),
    version: t("remoteDevices.onboarding.commands.version"),
  };
  const installCommand =
    remoteOS === "windows" ? commands.installWindows : commands.installUnix;

  return (
    <section className="overflow-hidden rounded-xl border border-border bg-card">
      <div className="flex items-start border-b border-border bg-muted/50">
        <ol className="grid min-w-0 flex-1 grid-cols-1 sm:grid-cols-3">
          <StepHeader
            number={1}
            activeStep={step}
            title={t("remoteDevices.onboarding.steps.install.title")}
            subtitle={
              step > 1
                ? t("remoteDevices.onboarding.steps.install.done")
                : t("remoteDevices.onboarding.steps.install.subtitle")
            }
          />
          <StepHeader
            number={2}
            activeStep={step}
            title={t("remoteDevices.onboarding.steps.service.title")}
            subtitle={
              step > 2
                ? t("remoteDevices.onboarding.steps.service.done")
                : t("remoteDevices.onboarding.steps.service.subtitle")
            }
          />
          <StepHeader
            number={3}
            activeStep={step}
            title={t("remoteDevices.onboarding.steps.pair.title")}
            subtitle={t("remoteDevices.onboarding.steps.pair.subtitle")}
          />
        </ol>
        {onDismiss ? (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            className="m-2 shrink-0"
            aria-label={t("remoteDevices.onboarding.dismiss")}
            onClick={onDismiss}
          >
            <X aria-hidden="true" />
          </Button>
        ) : null}
      </div>

      <div className="flex flex-col gap-5 p-4 sm:p-6">
        <div className="flex flex-col gap-2">
          <p className="font-mono text-2xs font-semibold uppercase tracking-[0.1em] text-primary-text">
            {t("remoteDevices.onboarding.stepLabel", {
              current: step,
              total: 3,
            })}
          </p>
          {step === 1 ? (
            <>
              <h2 className="text-lg font-semibold">
                {t("remoteDevices.onboarding.install.title")}
              </h2>
              <p className="text-sm leading-relaxed text-muted-foreground">
                {t("remoteDevices.onboarding.install.description")}
              </p>
            </>
          ) : step === 2 ? (
            <>
              <h2 className="text-lg font-semibold">
                {t("remoteDevices.onboarding.service.title")}
              </h2>
              <p className="text-sm leading-relaxed text-muted-foreground">
                {t("remoteDevices.onboarding.service.description")}
              </p>
            </>
          ) : (
            <>
              <h2 className="text-lg font-semibold">
                {t("remoteDevices.onboarding.pair.title")}
              </h2>
              <p className="text-sm leading-relaxed text-muted-foreground">
                {t("remoteDevices.onboarding.pair.description")}
              </p>
            </>
          )}
        </div>

        {step === 1 ? (
          <div className="flex flex-col gap-5">
            <div className="flex flex-col gap-2">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="text-sm font-medium">
                  {t("remoteDevices.onboarding.install.osLabel")}
                </span>
                <span className="text-xs text-muted-foreground">
                  {t("remoteDevices.onboarding.install.archHint")}
                </span>
              </div>
              <ToggleGroup
                type="single"
                value={remoteOS}
                variant="outline"
                spacing={1}
                aria-label={t("remoteDevices.onboarding.install.osLabel")}
                onValueChange={(value) => {
                  if (value) setRemoteOS(value as RemoteOS);
                }}
              >
                <ToggleGroupItem
                  value="linux"
                  aria-label={t("remoteDevices.onboarding.install.linux")}
                >
                  {t("remoteDevices.onboarding.install.linux")}
                </ToggleGroupItem>
                <ToggleGroupItem
                  value="macos"
                  aria-label={t("remoteDevices.onboarding.install.macos")}
                >
                  {t("remoteDevices.onboarding.install.macos")}
                </ToggleGroupItem>
                <ToggleGroupItem
                  value="windows"
                  aria-label={t("remoteDevices.onboarding.install.windows")}
                >
                  {t("remoteDevices.onboarding.install.windows")}
                </ToggleGroupItem>
              </ToggleGroup>
            </div>

            <div className="flex flex-col gap-2">
              <h3 className="text-sm font-semibold">
                {t("remoteDevices.onboarding.install.recommended")}
              </h3>
              <CommandCard
                label={
                  remoteOS === "windows"
                    ? t("remoteDevices.onboarding.install.powershell")
                    : t("remoteDevices.onboarding.install.terminal")
                }
                command={installCommand}
              />
              <CommandCard
                label={t("remoteDevices.onboarding.install.verify")}
                command={commands.version}
              />
            </div>

            <Separator />
            <div className="flex flex-wrap items-center justify-between gap-3">
              <Button variant="link" size="sm" asChild>
                <a href={MANUAL_INSTALL_URL} target="_blank" rel="noreferrer">
                  {t("remoteDevices.onboarding.install.manual")}
                </a>
              </Button>
              <Button type="button" onClick={() => setStep(2)}>
                {t("remoteDevices.onboarding.install.next")}
                <ArrowRight data-icon="inline-end" aria-hidden="true" />
              </Button>
            </div>
          </div>
        ) : null}

        {step === 2 ? (
          <div className="flex flex-col gap-5">
            <div className="flex flex-col gap-2">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="text-sm font-medium">
                  {t("remoteDevices.onboarding.service.modeLabel")}
                </span>
                <span className="text-xs text-muted-foreground">
                  {t("remoteDevices.onboarding.service.modeHint")}
                </span>
              </div>
              <ToggleGroup
                type="single"
                value={runMode}
                variant="outline"
                spacing={1}
                aria-label={t("remoteDevices.onboarding.service.modeLabel")}
                onValueChange={(value) => {
                  if (value) setRunMode(value as RunMode);
                }}
              >
                <ToggleGroupItem
                  value="background"
                  aria-label={t("remoteDevices.onboarding.service.background")}
                >
                  {t("remoteDevices.onboarding.service.background")}
                </ToggleGroupItem>
                <ToggleGroupItem
                  value="foreground"
                  aria-label={t("remoteDevices.onboarding.service.foreground")}
                >
                  {t("remoteDevices.onboarding.service.foreground")}
                </ToggleGroupItem>
              </ToggleGroup>
            </div>

            {runMode === "background" ? (
              <div className="flex flex-col gap-4">
                <CommandCard
                  label={t("remoteDevices.onboarding.service.installLabel")}
                  command={commands.serviceInstall}
                />
                <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                  <CommandCard
                    label={t("remoteDevices.onboarding.service.statusLabel")}
                    command={commands.serviceStatus}
                  />
                  <CommandCard
                    label={t("remoteDevices.onboarding.service.restartLabel")}
                    command={commands.serviceRestart}
                  />
                </div>
                <div className="flex flex-col gap-3">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <h3 className="text-sm font-semibold">
                      {t("remoteDevices.onboarding.service.diagnosticsTitle")}
                    </h3>
                    <span className="text-xs text-muted-foreground">
                      {t("remoteDevices.onboarding.service.diagnosticsHint")}
                    </span>
                  </div>
                  <ol className="flex flex-col gap-2 text-xs leading-relaxed text-muted-foreground">
                    <DiagnosticItem number={1}>
                      <Trans
                        i18nKey="remoteDevices.onboarding.service.diagnostics.first"
                        values={{
                          command: commands.serviceStatus,
                          status: commands.daemonRunning,
                        }}
                        components={{
                          command: (
                            <code
                              data-selectable-text="true"
                              className="select-text font-mono text-foreground"
                            />
                          ),
                          status: (
                            <code
                              data-selectable-text="true"
                              className="select-text font-mono text-foreground"
                            />
                          ),
                        }}
                      />
                    </DiagnosticItem>
                    <DiagnosticItem number={2}>
                      <Trans
                        i18nKey="remoteDevices.onboarding.service.diagnostics.second"
                        values={{ address: commands.listenAddress }}
                        components={{
                          address: (
                            <code
                              data-selectable-text="true"
                              className="select-text font-mono text-foreground"
                            />
                          ),
                        }}
                      />
                    </DiagnosticItem>
                    <DiagnosticItem number={3}>
                      {t("remoteDevices.onboarding.service.diagnostics.third")}
                    </DiagnosticItem>
                  </ol>
                </div>
              </div>
            ) : (
              <div className="flex flex-col gap-2">
                <CommandCard
                  label={t("remoteDevices.onboarding.install.terminal")}
                  command={commands.foregroundRun}
                />
                <p className="text-xs text-muted-foreground">
                  {t("remoteDevices.onboarding.service.foregroundDescription")}
                </p>
              </div>
            )}

            <Separator />
            <div className="flex justify-end">
              <Button type="button" onClick={() => setStep(3)}>
                {t("remoteDevices.onboarding.service.next")}
                <ArrowRight data-icon="inline-end" aria-hidden="true" />
              </Button>
            </div>
          </div>
        ) : null}

        {step === 3 ? (
          <div className="flex flex-col gap-4">
            <CommandCard
              label={t("remoteDevices.onboarding.pair.commandLabel")}
              command={commands.pair}
            />
            <div className="rounded-lg border border-border bg-muted/30 p-4">
              <DevicePairingForm
                actionsClassName="justify-between"
                cancelLabel={t("remoteDevices.onboarding.pair.back")}
                onCancel={() => setStep(2)}
                onSubmit={onSubmit}
                submitLabel={t("remoteDevices.onboarding.pair.submit")}
              />
            </div>
          </div>
        ) : null}
      </div>
    </section>
  );
}

function StepHeader({
  activeStep,
  number,
  subtitle,
  title,
}: {
  activeStep: OnboardingStep;
  number: OnboardingStep;
  subtitle: string;
  title: string;
}) {
  const complete = number < activeStep;
  const active = number === activeStep;

  return (
    <li
      aria-current={active ? "step" : undefined}
      className="flex min-w-0 items-start gap-3 border-b border-border p-4 last:border-b-0 sm:border-b-0 sm:border-r sm:last:border-r-0"
    >
      <span
        aria-hidden="true"
        className={cn(
          "flex size-6 shrink-0 items-center justify-center rounded-full bg-muted font-mono text-2xs font-semibold text-muted-foreground",
          active && "bg-primary text-primary-foreground",
          complete && "bg-status-running-bg text-status-running",
        )}
      >
        {complete ? (
          <Check className="size-3" />
        ) : (
          String(number).padStart(2, "0")
        )}
      </span>
      <span className="min-w-0">
        <span className="block truncate text-xs font-semibold">{title}</span>
        <span className="mt-0.5 block truncate text-2xs text-muted-foreground">
          {subtitle}
        </span>
      </span>
    </li>
  );
}

function DiagnosticItem({
  children,
  number,
}: {
  children: ReactNode;
  number: number;
}) {
  return (
    <li className="flex items-start gap-2">
      <span
        aria-hidden="true"
        className="flex size-5 shrink-0 items-center justify-center rounded-full bg-muted font-mono text-2xs text-muted-foreground"
      >
        {number}
      </span>
      <span>{children}</span>
    </li>
  );
}
