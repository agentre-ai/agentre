import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { DeviceTag } from "../device-tag";

describe("DeviceTag", () => {
  it("本地: deviceId 为空时显示 '本地' chip", () => {
    render(<DeviceTag deviceId="" deviceName="" online={false} />);
    expect(screen.getByText("本地")).toBeInTheDocument();
  });

  it("远端 online: 显示 deviceName, 不显示 offline", () => {
    render(<DeviceTag deviceId="7" deviceName="linux-srv" online />);
    expect(screen.getByText("linux-srv")).toBeInTheDocument();
    expect(screen.queryByText(/offline/i)).toBeNull();
  });

  it("远端 offline: 显示 deviceName + offline 后缀", () => {
    render(<DeviceTag deviceId="7" deviceName="mac-mini" online={false} />);
    expect(screen.getByText(/mac-mini/)).toBeInTheDocument();
    expect(screen.getByText(/offline/i)).toBeInTheDocument();
  });

  it("传入 className 时附加到根 span 上(不覆盖默认样式)", () => {
    const { container } = render(
      <DeviceTag
        deviceId=""
        deviceName=""
        online={false}
        className="my-test-class"
      />,
    );
    const span = container.querySelector("span");
    expect(span?.className).toContain("my-test-class");
    expect(span?.className).toContain("bg-primary-soft"); // 默认样式仍在
  });
});
