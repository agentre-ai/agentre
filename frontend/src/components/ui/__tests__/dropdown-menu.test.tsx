import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

describe("DropdownMenu", () => {
  it("uses a pointer cursor for actionable menu entries", () => {
    render(
      <DropdownMenu open>
        <DropdownMenuTrigger>打开菜单</DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem>重命名</DropdownMenuItem>
          <DropdownMenuCheckboxItem checked>显示归档</DropdownMenuCheckboxItem>
          <DropdownMenuRadioGroup value="compact">
            <DropdownMenuRadioItem value="compact">
              紧凑视图
            </DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>更多</DropdownMenuSubTrigger>
            <DropdownMenuSubContent>
              <DropdownMenuItem>导出</DropdownMenuItem>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        </DropdownMenuContent>
      </DropdownMenu>,
    );

    expect(screen.getByRole("menuitem", { name: "重命名" })).toHaveClass(
      "cursor-pointer",
    );
    expect(
      screen.getByRole("menuitemcheckbox", { name: "显示归档" }),
    ).toHaveClass("cursor-pointer");
    expect(screen.getByRole("menuitemradio", { name: "紧凑视图" })).toHaveClass(
      "cursor-pointer",
    );
    expect(screen.getByRole("menuitem", { name: "更多" })).toHaveClass(
      "cursor-pointer",
    );
  });

  it("highlights actionable menu entries on hover", () => {
    render(
      <DropdownMenu open>
        <DropdownMenuTrigger>打开菜单</DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem>重命名</DropdownMenuItem>
          <DropdownMenuItem variant="destructive">删除</DropdownMenuItem>
          <DropdownMenuCheckboxItem checked>显示归档</DropdownMenuCheckboxItem>
          <DropdownMenuRadioGroup value="compact">
            <DropdownMenuRadioItem value="compact">
              紧凑视图
            </DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>更多</DropdownMenuSubTrigger>
          </DropdownMenuSub>
        </DropdownMenuContent>
      </DropdownMenu>,
    );

    expect(screen.getByRole("menuitem", { name: "重命名" })).toHaveClass(
      "hover:bg-accent",
      "hover:text-accent-foreground",
    );
    expect(screen.getByRole("menuitem", { name: "删除" })).toHaveClass(
      "data-[variant=destructive]:hover:bg-destructive/10",
      "data-[variant=destructive]:hover:text-destructive",
    );
    expect(
      screen.getByRole("menuitemcheckbox", { name: "显示归档" }),
    ).toHaveClass("hover:bg-accent", "hover:text-accent-foreground");
    expect(screen.getByRole("menuitemradio", { name: "紧凑视图" })).toHaveClass(
      "hover:bg-accent",
      "hover:text-accent-foreground",
    );
    expect(screen.getByRole("menuitem", { name: "更多" })).toHaveClass(
      "hover:bg-accent",
      "hover:text-accent-foreground",
    );
  });
});
