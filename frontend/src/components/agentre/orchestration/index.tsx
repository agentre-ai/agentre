export function OrchestrationRun({
  runId,
  title,
}: {
  runId: number;
  title: string;
}) {
  return (
    <div
      data-testid="orchestration-run"
      data-run-id={runId}
      aria-label={title}
    />
  );
}
