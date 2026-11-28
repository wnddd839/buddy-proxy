// 分词器指纹 / 词表身份鉴定的载荷类型。
//
// 注意桶名：面板契约里写的是 digit / alpha，但服务端（internal/tokfp）
// 实际发的是 digits / ascii，且另有 sym 桶。这里两种都收，展示时统一映射
// 成中文名 —— 漏了任何一种都会让真实基线行显示成原始英文键。
export type TokBucket =
  | 'cjk'
  | 'digit'
  | 'digits'
  | 'alpha'
  | 'ascii'
  | 'sym'
  | 'tiny'
  | 'none';

export interface TokBaseline {
  model: string;
  bucket: TokBucket;
  fast: number;
  slow: number;
  lastRatio: number;
  samples: number;
  /** 由服务端按「当前值是否超出快/慢线」算好，前端只负责高亮，不重复判定 */
  deviating: boolean;
}

export interface TokBaselinesPayload {
  ok: boolean;
  enabled: boolean;
  count: number;
  baselines: TokBaseline[];
}

export interface TokIdentitySample {
  name: string;
  observed?: number;
  official: number;
  delta?: number;
  verdict?: string;
}

export interface TokTemplateCase {
  name: string;
  observed?: number;
  predicted: number;
  ok?: boolean;
}

export interface TokIdentityResult {
  model: string;
  site?: string;
  vocabulary: string;
  weight: string;
  note?: string;
  // 以下字段新契约里没列，但端点确实会返回（见 internal/gateway/tokidentity.go），
  // 保留它们是为了不丢掉 legacy 面板的「模板公式 / 样本明细」证据。
  accountId?: string;
  samples?: TokIdentitySample[];
  /** 后端是 bool（非指针）：没跑过模板校验时它也是 false 零值。
   *  展示时必须结合 vocabulary 判「未测」，不能直接信 false。 */
  templateOK?: boolean;
  templateDetail?: TokTemplateCase[];
  cost?: string;
}

export interface TokIdentityPayload {
  ok: boolean;
  count: number;
  results: TokIdentityResult[];
}

export interface TokTogglePayload {
  ok: boolean;
  enabled: boolean;
  error?: string;
}
