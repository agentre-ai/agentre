import type * as React from "react";

/**
 * indentStyle 是「变动」与「目录」两棵树共用的每层缩进量。两个模式在同一个面板里
 * 上下切换，缩进单位一旦分叉，切模式时同一批文件就会横向跳一下 —— 所以它是一份
 * 共享常量，而不是各自私有的同名函数。
 */
export function indentStyle(depth: number): React.CSSProperties {
  return { paddingLeft: `${8 + depth * 14}px` };
}
