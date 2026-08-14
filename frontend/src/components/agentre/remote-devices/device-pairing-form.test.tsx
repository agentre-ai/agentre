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

  // 以下三条随 AddDeviceDialog 一并迁来:配对表单现在是这些校验规则的唯一宿主。
  it("keeps the submit action disabled until the address and the code are both valid", async () => {
    const user = userEvent.setup();
    renderForm();
    const submit = screen.getByRole("button", { name: "Pair and verify" });

    expect(submit).toBeDisabled();
    await user.type(screen.getByLabelText("Address"), "ws://h/rpc");
    expect(submit).toBeDisabled();
    await user.type(screen.getByLabelText("Pairing Code"), "ABC2DE");
    expect(submit).toBeEnabled();
  });

  it("shows the address and pairing-code rules inline, and only once something was typed", async () => {
    const user = userEvent.setup();
    renderForm();

    expect(screen.getByText("6-character pairing code")).toBeInTheDocument();
    expect(
      screen.queryByText(
        "Address must end with /rpc, e.g. ws://192.168.1.100:7456/rpc",
      ),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText("Pairing code must be exactly 6 characters"),
    ).not.toBeInTheDocument();

    await user.type(
      screen.getByLabelText("Address"),
      "ws://192.168.1.100:7456",
    );
    expect(
      screen.getByText(
        "Address must end with /rpc, e.g. ws://192.168.1.100:7456/rpc",
      ),
    ).toBeInTheDocument();

    await user.type(screen.getByLabelText("Pairing Code"), "ABCDE");
    expect(
      screen.getByText("Pairing code must be exactly 6 characters"),
    ).toBeInTheDocument();
  });

  it("accepts any six characters as the pairing code, without alphabet validation", async () => {
    const user = userEvent.setup();
    renderForm();

    await user.type(screen.getByLabelText("Address"), "ws://h/rpc");
    await user.type(screen.getByLabelText("Pairing Code"), "o01i89");

    expect(screen.getByLabelText("Pairing Code")).toHaveValue("O01I89");
    expect(
      screen.getByRole("button", { name: "Pair and verify" }),
    ).toBeEnabled();
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
