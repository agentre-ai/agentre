import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { DevicePairingForm } from "./device-pairing-form";

function renderForm(onSubmit = vi.fn().mockResolvedValue(undefined)) {
  render(
    <DevicePairingForm
      cancelLabel="Back to service"
      onCancel={() => {}}
      onSubmit={onSubmit}
      submitLabel="Pair and verify"
    />,
  );
  return onSubmit;
}

describe("DevicePairingForm", () => {
  it("reuses the existing URL, code, derived-name, and TLS request contract", async () => {
    const user = userEvent.setup();
    const onSubmit = renderForm();
    const submit = screen.getByRole("button", { name: "Pair and verify" });

    expect(submit).toBeDisabled();
    await user.type(screen.getByLabelText("Address"), "ws://host:7456/rpc");
    await user.type(screen.getByLabelText("Pairing Code"), "abc2def");
    expect(screen.getByLabelText("Pairing Code")).toHaveValue("ABC2DE");
    expect(submit).toBeEnabled();

    await user.click(submit);

    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith({
        url: "ws://host:7456/rpc",
        pairingCode: "ABC2DE",
        displayName: "host",
        tlsMode: "default",
        tlsCertPEM: "",
      }),
    );
  });

  it("shows the concrete submit error and retains every entered value", async () => {
    const user = userEvent.setup();
    const onSubmit = vi
      .fn()
      .mockRejectedValue(new Error("Pairing code expired"));
    renderForm(onSubmit);

    await user.type(screen.getByLabelText("Address"), "wss://host:7456/rpc");
    await user.type(screen.getByLabelText("Pairing Code"), "ABC2DE");
    await user.type(
      screen.getByLabelText("Display Name (optional)"),
      "Build box",
    );
    await user.click(screen.getByRole("button", { name: "Pair and verify" }));

    expect(await screen.findByText("Pairing code expired")).toBeInTheDocument();
    expect(screen.getByLabelText("Address")).toHaveValue("wss://host:7456/rpc");
    expect(screen.getByLabelText("Pairing Code")).toHaveValue("ABC2DE");
    expect(screen.getByLabelText("Display Name (optional)")).toHaveValue(
      "Build box",
    );
  });
});
