import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { MessageRow, MessageCopyButton } from "./message-row";

// 隔离剪贴板副作用：只断言 copyTextWithToast 被以正文调用。
const copySpy = vi.fn();
vi.mock("@/lib/clipboard-toast", () => ({
  copyTextWithToast: (text: string, opts: unknown) => {
    copySpy(text, opts);
    return Promise.resolve(true);
  },
}));

describe("MessageRow", () => {
  it("内置彩色头像用规范尺寸 size-7(锁住一致性)", () => {
    render(
      <MessageRow avatarName="后端" avatarColor="agent-2" name="后端">
        正文
      </MessageRow>,
    );
    const avatar = screen.getByLabelText("后端");
    expect(avatar.className).toContain("size-7");
    expect(screen.getByText("正文")).toBeInTheDocument();
  });

  it("传入 avatar 槽时用自定义头像、不渲染内置头像", () => {
    render(
      <MessageRow
        avatar={<span aria-label="me-pill">我</span>}
        avatarName="后端"
        name={null}
      >
        正文
      </MessageRow>,
    );
    expect(screen.getByLabelText("me-pill")).toBeInTheDocument();
    expect(screen.queryByLabelText("后端")).toBeNull();
  });

  it("footer 槽被渲染", () => {
    render(
      <MessageRow avatarName="后端" name="后端" footer={<span>页脚</span>}>
        正文
      </MessageRow>,
    );
    expect(screen.getByText("页脚")).toBeInTheDocument();
  });
});

describe("MessageCopyButton", () => {
  it("点击以正文文本调用 copyTextWithToast", () => {
    copySpy.mockClear();
    render(<MessageCopyButton text="hello world" />);
    fireEvent.click(screen.getByRole("button"));
    expect(copySpy).toHaveBeenCalledWith("hello world", expect.any(Object));
  });

  it("正文为空时不渲染", () => {
    const { container } = render(<MessageCopyButton text="" />);
    expect(container.querySelector("button")).toBeNull();
  });
});
