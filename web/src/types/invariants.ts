// 静默失效体检的数据契约。
//
// 后端 /direct-admin/api/invariants 在「启用」与「未启用」两种状态下返回
// **同一组键**（缺字段会被前端当成「0 命中」而不是「未启用」），所以这里
// 除 specs 外全部必填；specs 只在启用时下发。

/** 只有两种级别：info 只记面板，violation 才算「该有人处理」。 */
export type FindingSeverity = 'violation' | 'info';

export interface Finding {
  invariant: string;
  detail: string;
  severity?: string;
  subject?: string;
  at?: number;
  why?: string;
  evidence?: Record<string, unknown>;
  suppressed?: number;
}

export interface InvariantSpec {
  id: string;
  title: string;
  why: string;
  severity?: string;
}

export interface InvariantsPayload {
  ok: boolean;
  enabled: boolean;
  findings: Finding[];
  counts: Record<string, number>;
  dropped: number;
  suppressed: number;
  writeErrors: number;
  coverage: string;
  specs?: InvariantSpec[];
}
