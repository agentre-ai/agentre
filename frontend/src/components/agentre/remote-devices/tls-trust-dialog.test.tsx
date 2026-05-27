import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { TLSTrustDialog } from "./tls-trust-dialog";

const PEM = "-----BEGIN CERTIFICATE-----\nMIIBxxxx\n-----END CERTIFICATE-----";

describe("TLSTrustDialog", () => {
  it("应用 with default mode passes ('','')", async () => {
    const onApply = vi.fn();
    render(
      <TLSTrustDialog
        open
        initialMode="default"
        initialPEM=""
        onClose={() => {}}
        onApply={onApply}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "应用" }));
    await waitFor(() => expect(onApply).toHaveBeenCalledWith("default", ""));
  });

  it("pin-cert without PEM shows inline error and does not call onApply", async () => {
    const user = userEvent.setup();
    const onApply = vi.fn();
    render(
      <TLSTrustDialog
        open
        initialMode="default"
        initialPEM=""
        onClose={() => {}}
        onApply={onApply}
      />,
    );
    await user.click(screen.getByText("Pin 证书"));
    await user.click(screen.getByRole("button", { name: "应用" }));
    await waitFor(() =>
      expect(screen.getByText("请粘贴 PEM 内容")).toBeInTheDocument(),
    );
    expect(onApply).not.toHaveBeenCalled();
  });

  it("pin-cert with valid PEM calls onApply with mode+pem", async () => {
    const onApply = vi.fn();
    render(
      <TLSTrustDialog
        open
        initialMode="pin-cert"
        initialPEM=""
        onClose={() => {}}
        onApply={onApply}
      />,
    );
    const ta = screen.getByPlaceholderText(/BEGIN CERTIFICATE/);
    fireEvent.change(ta, { target: { value: PEM } });
    fireEvent.click(screen.getByRole("button", { name: "应用" }));
    await waitFor(() => expect(onApply).toHaveBeenCalledWith("pin-cert", PEM));
  });

  it("skip-verify renders destructive label", () => {
    render(
      <TLSTrustDialog
        open
        initialMode="skip-verify"
        initialPEM=""
        onClose={() => {}}
        onApply={() => {}}
      />,
    );
    expect(screen.getByText("不推荐")).toBeInTheDocument();
  });

  it("switching to ca-bundle without PEM blocks apply", async () => {
    const user = userEvent.setup();
    const onApply = vi.fn();
    render(
      <TLSTrustDialog
        open
        initialMode="default"
        initialPEM=""
        onClose={() => {}}
        onApply={onApply}
      />,
    );
    await user.click(screen.getByText("CA 证书包"));
    await user.click(screen.getByRole("button", { name: "应用" }));
    await waitFor(() =>
      expect(screen.getByText("请粘贴 PEM 内容")).toBeInTheDocument(),
    );
    expect(onApply).not.toHaveBeenCalled();
  });
});
