import React from 'react';

interface ScopeNoteProps {
  children: React.ReactNode;
  id?: string;
  title?: string;
}

/**
 * 指标口径说明条。存在的意义：同一个数字在不同页面口径不同时，
 * 必须当场说明是「进程内累计 / 落盘统计 / 1h 窗口 / 草稿」中的哪一种。
 * 旧面板的这类说明在 v0.8.0 的 React 重写里整批丢失，这里补回。
 */
export function ScopeNote({ children, id, title }: ScopeNoteProps) {
  return (
    <p
      id={id}
      title={title}
      className="text-[11px] leading-relaxed text-[var(--fg-3)] m-0 px-0.5"
    >
      {children}
    </p>
  );
}
