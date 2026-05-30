import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { RemoteFsPicker } from "./remote-fs-picker";

vi.mock("../../../wailsjs/go/app/App", () => ({
  RemoteFsListDir: vi.fn(),
  RemoteFsMkdir: vi.fn(),
}));

import { RemoteFsListDir, RemoteFsMkdir } from "../../../wailsjs/go/app/App";

const mockedList = RemoteFsListDir as unknown as ReturnType<typeof vi.fn>;
const mockedMkdir = RemoteFsMkdir as unknown as ReturnType<typeof vi.fn>;

function setup(initialPath = "/home/me") {
  const onPick = vi.fn();
  const onOpenChange = vi.fn();
  const utils = render(
    <RemoteFsPicker
      open
      onOpenChange={onOpenChange}
      deviceID="7"
      deviceName="DEV-A"
      mode="dir"
      initialPath={initialPath}
      onPick={onPick}
    />,
  );
  return { ...utils, onPick, onOpenChange };
}

beforeEach(() => {
  mockedList.mockReset();
  mockedMkdir.mockReset();
});
afterEach(() => {
  vi.clearAllMocks();
});

describe("<RemoteFsPicker>", () => {
  it("renders list with dirs first + hides dotfiles by default", async () => {
    mockedList.mockResolvedValueOnce({
      path: "/home/me",
      truncated: false,
      entries: [
        { name: ".secret", isDir: false, size: 0, mtime: 0 },
        { name: "z-file.txt", isDir: false, size: 1, mtime: 0 },
        { name: "Work", isDir: true, size: 0, mtime: 0 },
      ],
    });
    setup();
    await waitFor(() =>
      expect(screen.getByTestId("entry-Work")).toBeInTheDocument(),
    );
    expect(screen.queryByTestId("entry-.secret")).not.toBeInTheDocument();
    const rows = screen.getAllByText(/Work|z-file\.txt/);
    expect(rows[0]).toHaveTextContent("Work");
  });

  it("truncated banner renders", async () => {
    mockedList.mockResolvedValueOnce({
      path: "/home/me",
      truncated: true,
      entries: [],
    });
    setup();
    await waitFor(() =>
      expect(screen.getByText(/Some items are not listed/)).toBeInTheDocument(),
    );
  });

  it("filter narrows entries without calling listDir", async () => {
    mockedList.mockResolvedValueOnce({
      path: "/home/me",
      truncated: false,
      entries: [
        { name: "alpha", isDir: true, size: 0, mtime: 0 },
        { name: "beta", isDir: true, size: 0, mtime: 0 },
      ],
    });
    setup();
    await waitFor(() =>
      expect(screen.getByTestId("entry-alpha")).toBeInTheDocument(),
    );
    fireEvent.change(screen.getByLabelText("Filter current folder"), {
      target: { value: "bet" },
    });
    expect(screen.queryByTestId("entry-alpha")).not.toBeInTheDocument();
    expect(screen.getByTestId("entry-beta")).toBeInTheDocument();
    expect(mockedList).toHaveBeenCalledTimes(1);
  });

  it("show hidden toggle reveals dotfiles", async () => {
    mockedList.mockResolvedValueOnce({
      path: "/home/me",
      truncated: false,
      entries: [{ name: ".secret", isDir: false, size: 0, mtime: 0 }],
    });
    setup();
    await waitFor(() => expect(mockedList).toHaveBeenCalled());
    fireEvent.click(screen.getByLabelText("Show hidden files"));
    expect(screen.getByTestId("entry-.secret")).toBeInTheDocument();
  });

  it("mkdir happy: reload + select new + footer path picks it", async () => {
    mockedList
      .mockResolvedValueOnce({
        path: "/home/me",
        truncated: false,
        entries: [],
      })
      .mockResolvedValueOnce({
        path: "/home/me",
        truncated: false,
        entries: [{ name: "newdir", isDir: true, size: 0, mtime: 0 }],
      });
    mockedMkdir.mockResolvedValueOnce({ path: "/home/me/newdir" });
    const { onPick, onOpenChange } = setup();
    await waitFor(() => expect(mockedList).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole("button", { name: /New/ }));
    fireEvent.change(screen.getByPlaceholderText("folder name"), {
      target: { value: "newdir" },
    });
    fireEvent.keyDown(screen.getByPlaceholderText("folder name"), {
      key: "Enter",
    });
    await waitFor(() =>
      expect(mockedMkdir).toHaveBeenCalledWith("7", "/home/me", "newdir"),
    );
    await waitFor(() =>
      expect(screen.getByTestId("entry-newdir")).toBeInTheDocument(),
    );
    fireEvent.click(screen.getByRole("button", { name: "Select This Folder" }));
    expect(onPick).toHaveBeenCalledWith("/home/me/newdir");
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("mkdir invalid name: blocks RPC and shows error", async () => {
    mockedList.mockResolvedValueOnce({
      path: "/home/me",
      truncated: false,
      entries: [],
    });
    setup();
    await waitFor(() => expect(mockedList).toHaveBeenCalled());
    fireEvent.click(screen.getByRole("button", { name: /New/ }));
    fireEvent.change(screen.getByPlaceholderText("folder name"), {
      target: { value: "a/b" },
    });
    fireEvent.keyDown(screen.getByPlaceholderText("folder name"), {
      key: "Enter",
    });
    expect(mockedMkdir).not.toHaveBeenCalled();
    expect(screen.getByText("Invalid name")).toBeInTheDocument();
  });

  it("mkdir accepts internal spaces (matches backend pathguard)", async () => {
    mockedList
      .mockResolvedValueOnce({
        path: "/home/me",
        truncated: false,
        entries: [],
      })
      .mockResolvedValueOnce({
        path: "/home/me",
        truncated: false,
        entries: [{ name: "my projects", isDir: true, size: 0, mtime: 0 }],
      });
    mockedMkdir.mockResolvedValueOnce({ path: "/home/me/my projects" });
    setup();
    await waitFor(() => expect(mockedList).toHaveBeenCalled());
    fireEvent.click(screen.getByRole("button", { name: /New/ }));
    fireEvent.change(screen.getByPlaceholderText("folder name"), {
      target: { value: "my projects" },
    });
    fireEvent.keyDown(screen.getByPlaceholderText("folder name"), {
      key: "Enter",
    });
    await waitFor(() =>
      expect(mockedMkdir).toHaveBeenCalledWith("7", "/home/me", "my projects"),
    );
  });

  it("listDir error shows banner", async () => {
    mockedList.mockRejectedValueOnce(new Error("boom"));
    setup();
    await waitFor(() => expect(screen.getByText(/boom/)).toBeInTheDocument());
  });

  it("default footer path = currentPath; selecting dir narrows it", async () => {
    mockedList.mockResolvedValueOnce({
      path: "/home/me",
      truncated: false,
      entries: [{ name: "Work", isDir: true, size: 0, mtime: 0 }],
    });
    const { onPick } = setup();
    await waitFor(() =>
      expect(screen.getByTestId("entry-Work")).toBeInTheDocument(),
    );
    fireEvent.click(screen.getByRole("button", { name: "Select This Folder" }));
    expect(onPick).toHaveBeenLastCalledWith("/home/me");
    fireEvent.click(screen.getByTestId("entry-Work"));
    fireEvent.click(screen.getByRole("button", { name: "Select This Folder" }));
    expect(onPick).toHaveBeenLastCalledWith("/home/me/Work");
  });
});
