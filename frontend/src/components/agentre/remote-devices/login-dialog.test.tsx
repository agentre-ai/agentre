import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  act,
} from "@testing-library/react";

const browserOpenURLMock = vi.fn();
vi.mock("../../../../wailsjs/runtime/runtime", () => ({
  BrowserOpenURL: (url: string) => browserOpenURLMock(url),
}));

import { LoginDialog, type StartLoginResult } from "./login-dialog";

const RESULT: StartLoginResult = {
  DeviceCode: "device-abc",
  UserCode: "ABCD-1234",
  VerificationURI: "https://hub.example.com/device",
  VerificationURIComplete: "https://hub.example.com/device?code=ABCD-1234",
  Interval: 5,
  ExpiresIn: 900,
};

function renderDialog(
  overrides: Partial<Parameters<typeof LoginDialog>[0]> = {},
) {
  const props = {
    open: true,
    initialUrl: "",
    onClose: vi.fn(),
    onLoggedIn: vi.fn(),
    checkURL: vi.fn().mockResolvedValue("0.3.0"),
    startLogin: vi.fn().mockResolvedValue(RESULT),
    pollLoginToken: vi.fn().mockResolvedValue(false),
    cancelLogin: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
  render(<LoginDialog {...props} />);
  return props;
}

async function startFlow(props: ReturnType<typeof renderDialog>) {
  fireEvent.change(screen.getByPlaceholderText(/hub\.example\.com/), {
    target: { value: "https://hub.example.com" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  await waitFor(() => expect(props.startLogin).toHaveBeenCalled());
}

describe("LoginDialog", () => {
  beforeEach(() => {
    browserOpenURLMock.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  // a) starting login shows the user code + verification URL, and offers an
  // action that opens the browser.
  it("shows the user code and verification URL after starting, and opens the browser on demand", async () => {
    const props = renderDialog();
    await startFlow(props);

    await waitFor(() =>
      expect(screen.getByText("ABCD-1234")).toBeInTheDocument(),
    );
    expect(
      screen.getByText("https://hub.example.com/device"),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Open Browser" }));
    expect(browserOpenURLMock).toHaveBeenCalledWith(
      "https://hub.example.com/device?code=ABCD-1234",
    );
  });

  // b) polling runs at the returned Interval and, on success, the UI moves
  // to the logged-in state without further user action.
  it("polls at the returned Interval and auto-completes on success", async () => {
    vi.useFakeTimers();
    const pollLoginToken = vi
      .fn()
      .mockResolvedValueOnce(false)
      .mockResolvedValueOnce(false)
      .mockResolvedValueOnce(true);
    const onLoggedIn = vi.fn();
    const onClose = vi.fn();
    const props = renderDialog({ pollLoginToken, onLoggedIn, onClose });

    await act(async () => {
      fireEvent.change(screen.getByPlaceholderText(/hub\.example\.com/), {
        target: { value: "https://hub.example.com" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(props.startLogin).toHaveBeenCalled();

    // No poll before a full interval has elapsed.
    expect(pollLoginToken).not.toHaveBeenCalled();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(pollLoginToken).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(pollLoginToken).toHaveBeenCalledTimes(2);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(pollLoginToken).toHaveBeenCalledTimes(3);
    expect(onLoggedIn).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();

    // No further polling once logged in.
    pollLoginToken.mockClear();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(20_000);
    });
    expect(pollLoginToken).not.toHaveBeenCalled();
  });

  // b2) 卸载发生在 startLogin 还在飞的时候:迟到的那次 start 不得再装一个
  // 谁也清不掉的 interval。stopPolling 只清**已经存在**的 timer,而 start 过去
  // 是在 await 之后才读 attemptRef —— 读到的是卸载清理刚 +1 过的那个值,于是
  // 身份校验反而放行,轮询就此每 5s 跑到进程结束(还会把 onLoggedIn 打进已死的父组件)。
  it("does not leave a polling interval behind when it unmounts mid-start", async () => {
    vi.useFakeTimers();
    let releaseStart: (r: StartLoginResult) => void = () => {};
    const startLogin = vi.fn(
      () =>
        new Promise<StartLoginResult>((resolve) => {
          releaseStart = resolve;
        }),
    );
    const pollLoginToken = vi.fn().mockResolvedValue(false);
    const onLoggedIn = vi.fn();
    const { unmount } = render(
      <LoginDialog
        open
        initialUrl=""
        onClose={vi.fn()}
        onLoggedIn={onLoggedIn}
        checkURL={vi.fn().mockResolvedValue("0.3.0")}
        startLogin={startLogin}
        pollLoginToken={pollLoginToken}
        cancelLogin={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    await act(async () => {
      fireEvent.change(screen.getByPlaceholderText(/hub\.example\.com/), {
        target: { value: "https://hub.example.com" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(startLogin).toHaveBeenCalled();

    // 用户在这一次网络往返还没回来的时候离开了设置页。
    unmount();
    await act(async () => {
      releaseStart(RESULT);
      await Promise.resolve();
      await Promise.resolve();
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });
    expect(pollLoginToken).not.toHaveBeenCalled();
    expect(onLoggedIn).not.toHaveBeenCalled();
  });

  // c) cancelling stops the polling and calls ServerCancelLogin.
  it("cancelling stops polling and calls cancelLogin", async () => {
    vi.useFakeTimers();
    const pollLoginToken = vi.fn().mockResolvedValue(false);
    const cancelLogin = vi.fn().mockResolvedValue(undefined);
    const props = renderDialog({ pollLoginToken, cancelLogin });

    await act(async () => {
      fireEvent.change(screen.getByPlaceholderText(/hub\.example\.com/), {
        target: { value: "https://hub.example.com" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(props.startLogin).toHaveBeenCalled();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(pollLoginToken).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    // cancelLogin() is invoked synchronously inside the click handler (only
    // its resolution is async) — asserting immediately avoids waitFor's
    // setTimeout-based polling, which fake timers would otherwise stall.
    expect(cancelLogin).toHaveBeenCalled();

    pollLoginToken.mockClear();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });
    expect(pollLoginToken).not.toHaveBeenCalled();
  });

  // 取消只清了 interval,在飞的那一次 pollLoginToken 还挂着。它随后 resolve(true)
  // ——用户在浏览器里已经批准过、只是又按了取消——旧代码照样跑 onLoggedIn()+onClose(),
  // 于是一次被明确取消(且已经发过 cancelLogin)的登录把桌面端标成已登录。
  it("ignores an in-flight poll that settles after the user cancelled", async () => {
    let releasePoll: (done: boolean) => void = () => {};
    const pollLoginToken = vi.fn().mockImplementation(
      () =>
        new Promise<boolean>((resolve) => {
          releasePoll = resolve;
        }),
    );
    const cancelLogin = vi.fn().mockResolvedValue(undefined);
    const props = renderDialog({ pollLoginToken, cancelLogin });

    vi.useFakeTimers();
    await act(async () => {
      fireEvent.change(screen.getByPlaceholderText(/hub\.example\.com/), {
        target: { value: "https://hub.example.com" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
      await Promise.resolve();
      await Promise.resolve();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(pollLoginToken).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(cancelLogin).toHaveBeenCalled();
    expect(props.onLoggedIn).not.toHaveBeenCalled();

    // 取消之后那一次在飞的轮询才落定,并且带回「已批准」。
    await act(async () => {
      releasePoll(true);
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(props.onLoggedIn).not.toHaveBeenCalled();
  });

  // e) an error from start surfaces to the user rather than a silent spinner.
  it("surfaces a start error and lets the user retry", async () => {
    const startLogin = vi
      .fn()
      .mockRejectedValue(new Error("server: unreachable"));
    renderDialog({ startLogin });

    fireEvent.change(screen.getByPlaceholderText(/hub\.example\.com/), {
      target: { value: "https://hub.example.com" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    await waitFor(() =>
      expect(
        screen.getByText(
          "Can't reach the server. Check the URL and try again.",
        ),
      ).toBeInTheDocument(),
    );
    // Not stuck showing "Connecting..." — the action is available again.
    expect(screen.getByRole("button", { name: "Continue" })).not.toBeDisabled();
  });

  // e) an error from poll surfaces to the user rather than a silent spinner.
  it("surfaces a poll error", async () => {
    vi.useFakeTimers();
    const pollLoginToken = vi
      .fn()
      .mockRejectedValue(new Error("server: access denied"));
    const props = renderDialog({ pollLoginToken });

    await act(async () => {
      fireEvent.change(screen.getByPlaceholderText(/hub\.example\.com/), {
        target: { value: "https://hub.example.com" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(props.startLogin).toHaveBeenCalled();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });

    expect(screen.getByText("Sign-in was declined.")).toBeInTheDocument();

    pollLoginToken.mockClear();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });
    expect(pollLoginToken).not.toHaveBeenCalled();
  });

  // grounding fact: expiry uses ExpiresIn — stop polling locally once the
  // device code's lifetime has elapsed, even if the server never says so.
  it("stops polling once ExpiresIn elapses without a successful poll", async () => {
    vi.useFakeTimers();
    const pollLoginToken = vi.fn().mockResolvedValue(false);
    const startLogin = vi.fn().mockResolvedValue({
      ...RESULT,
      Interval: 5,
      ExpiresIn: 12,
    });
    const props = renderDialog({ pollLoginToken, startLogin });

    await act(async () => {
      fireEvent.change(screen.getByPlaceholderText(/hub\.example\.com/), {
        target: { value: "https://hub.example.com" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(props.startLogin).toHaveBeenCalled();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(pollLoginToken).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000); // t=10s, still < 12s expiry
    });
    expect(pollLoginToken).toHaveBeenCalledTimes(2);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000); // t=15s, past 12s expiry
    });
    expect(
      screen.getByText("The code expired. Sign in again."),
    ).toBeInTheDocument();

    pollLoginToken.mockClear();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });
    expect(pollLoginToken).not.toHaveBeenCalled();
  });
});
