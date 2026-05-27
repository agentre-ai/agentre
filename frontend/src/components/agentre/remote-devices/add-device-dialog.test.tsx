import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

// Stub out use-remote-devices so the wailsjs transitive import is avoided.
vi.mock("./use-remote-devices", () => ({}));

import { AddDeviceDialog } from "./add-device-dialog";

describe("AddDeviceDialog", () => {
  it("配对 button stays disabled until URL + code are valid", () => {
    render(
      <AddDeviceDialog open onClose={() => {}} onSubmit={async () => {}} />,
    );
    const btn = screen.getByRole("button", { name: "配对" });
    expect(btn).toBeDisabled();
    fireEvent.change(screen.getByPlaceholderText(/192\.168/), {
      target: { value: "ws://h/rpc" },
    });
    expect(btn).toBeDisabled();
    fireEvent.change(screen.getByPlaceholderText("ABC2DE"), {
      target: { value: "ABC2DE" },
    });
    expect(btn).not.toBeDisabled();
  });

  it("keeps six-character pairing codes without alphabet validation", () => {
    render(
      <AddDeviceDialog open onClose={() => {}} onSubmit={async () => {}} />,
    );
    const codeInput = screen.getByPlaceholderText("ABC2DE") as HTMLInputElement;
    fireEvent.change(screen.getByPlaceholderText(/192\.168/), {
      target: { value: "ws://h/rpc" },
    });
    fireEvent.change(codeInput, { target: { value: "O01I89" } });

    expect(codeInput.value).toBe("O01I89");
    expect(screen.getByRole("button", { name: "配对" })).not.toBeDisabled();
  });

  it("shows concise pairing code length help", () => {
    render(
      <AddDeviceDialog open onClose={() => {}} onSubmit={async () => {}} />,
    );

    expect(screen.getByText("6 字符配对码")).toBeInTheDocument();
  });

  it("submits the request and resets on success", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(<AddDeviceDialog open onClose={onClose} onSubmit={onSubmit} />);
    fireEvent.change(screen.getByPlaceholderText(/192\.168/), {
      target: { value: "ws://linux-srv.local:7456/rpc" },
    });
    fireEvent.change(screen.getByPlaceholderText("ABC2DE"), {
      target: { value: "ABC2DE" },
    });
    fireEvent.click(screen.getByRole("button", { name: "配对" }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    const payload = onSubmit.mock.calls[0][0];
    expect(payload.url).toBe("ws://linux-srv.local:7456/rpc");
    expect(payload.pairingCode).toBe("ABC2DE");
    expect(payload.displayName).toBe("linux-srv");
    expect(payload.tlsMode).toBe("default");
  });

  it("shows inline error and keeps dialog open on submit failure", async () => {
    const onSubmit = vi.fn().mockRejectedValue(new Error("配对码已过期"));
    render(<AddDeviceDialog open onClose={() => {}} onSubmit={onSubmit} />);
    fireEvent.change(screen.getByPlaceholderText(/192\.168/), {
      target: { value: "ws://h/rpc" },
    });
    fireEvent.change(screen.getByPlaceholderText("ABC2DE"), {
      target: { value: "ABC2DE" },
    });
    fireEvent.click(screen.getByRole("button", { name: "配对" }));
    await waitFor(() =>
      expect(screen.getByText("配对码已过期")).toBeInTheDocument(),
    );
  });
});
