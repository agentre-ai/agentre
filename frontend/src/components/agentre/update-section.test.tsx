import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const runtimeMocks = vi.hoisted(() => ({
  BrowserOpenURL: vi.fn(),
  EventsOff: vi.fn(),
  EventsOn: vi.fn(),
}));

vi.mock("../../../wailsjs/runtime/runtime", () => runtimeMocks);

import { UpdateSection } from "./update-section";

const REPOSITORY_URL = "https://github.com/agentre-ai/agentre";

function installUpdateBindings(overrides?: {
  getUpdateChannel?: () => Promise<unknown>;
  getDownloadMirror?: () => Promise<unknown>;
  getAvailableMirrors?: () => Promise<unknown>;
}) {
  Object.defineProperty(window, "go", {
    configurable: true,
    value: {
      app: {
        App: {
          GetUpdateChannel:
            overrides?.getUpdateChannel ??
            vi.fn(() => Promise.resolve("stable")),
          GetDownloadMirror:
            overrides?.getDownloadMirror ?? vi.fn(() => Promise.resolve("")),
          GetAvailableMirrors:
            overrides?.getAvailableMirrors ??
            vi.fn(() =>
              Promise.resolve([{ id: "github", name: "GitHub", url: "" }]),
            ),
        },
      },
    },
  });
}

beforeEach(() => {
  runtimeMocks.BrowserOpenURL.mockReset();
  runtimeMocks.EventsOff.mockReset();
  runtimeMocks.EventsOn.mockReset();
  installUpdateBindings();
});

describe("UpdateSection repository address", () => {
  it("Given the update page loads, When users inspect current version, Then the repository address is visible and opens externally", async () => {
    render(<UpdateSection />);

    const link = await screen.findByRole("link", { name: REPOSITORY_URL });
    expect(link).toHaveAttribute("href", REPOSITORY_URL);

    fireEvent.click(link);

    expect(runtimeMocks.BrowserOpenURL).toHaveBeenCalledWith(REPOSITORY_URL);
  });

  it("Given update settings fail to load, When the page settles, Then the repository address remains available", async () => {
    installUpdateBindings({
      getUpdateChannel: vi.fn(() => Promise.reject(new Error("settings down"))),
    });

    render(<UpdateSection />);

    await waitFor(() => expect(runtimeMocks.EventsOn).toHaveBeenCalled());
    expect(
      screen.getByRole("link", { name: REPOSITORY_URL }),
    ).toBeInTheDocument();
  });
});
