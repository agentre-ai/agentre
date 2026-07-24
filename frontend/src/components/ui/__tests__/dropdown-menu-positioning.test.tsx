import { render } from "@testing-library/react";
import type * as React from "react";
import { describe, expect, it, vi } from "vitest";

const primitiveProps = vi.hoisted(() => ({
  content: null as Record<string, unknown> | null,
}));

vi.mock("radix-ui", () => {
  const Primitive = ({ children }: { children?: React.ReactNode }) => children;
  const Content = (props: Record<string, unknown>) => {
    primitiveProps.content = props;
    return <div>{props.children as React.ReactNode}</div>;
  };

  return {
    DropdownMenu: new Proxy(
      {},
      {
        get: (_target, property) =>
          property === "Content" ? Content : Primitive,
      },
    ),
  };
});

import { DropdownMenuContent } from "@/components/ui/dropdown-menu";

describe("DropdownMenuContent positioning", () => {
  it("Given a menu is close to a viewport edge, When it is positioned, Then the shared component enables flipping with a safe boundary", () => {
    render(<DropdownMenuContent>Agent</DropdownMenuContent>);

    expect(primitiveProps.content).toMatchObject({
      avoidCollisions: true,
      collisionPadding: 8,
      sticky: "always",
    });
  });
});
