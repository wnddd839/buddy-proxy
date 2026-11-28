import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import type { ModelRouteRule, TitleRoute } from '@/types/api';
import {
  DRAFTS_SCHEMA_VERSION,
  ROUTES_DRAFT_KEY,
  TITLE_DRAFT_KEY,
  clearDraft,
  getSessionStorage,
  normalizeRulesDraft,
  normalizeTitleDraft,
  parseStoredDraft,
  readDraft,
  rulesFingerprint,
  shouldRestoreDraft,
  titleFingerprint,
  titleFromPayload,
  writeDraft,
  type DraftStorageLike,
} from '@/lib/drafts';

// 模型页草稿持久化（sessionStorage）
//
// 背景：ModelsPage 在切侧边栏标签时会卸载（App 每个 tab 独立条件渲染）。
// 草稿原来只存在组件 state 里，切页即丢。这里验证两件事：
//   1. 纯 helper 自己的契约（版本化 key、往返、坏数据忽略并删除、恢复判据）；
//   2. ModelsPage 里 hydrate / 持久化 / 清除的接线还在（源码级，页面没有 DOM
//      测试环境，且渲染节点断言无法覆盖 effect 顺序）。

const here = dirname(fileURLToPath(import.meta.url));
const src = (p: string) => readFileSync(join(here, '..', p), 'utf8');
const models = src('pages/ModelsPage.tsx');

/** 取 text 中 start..end（含端点）的片段；找不到即测试失败。 */
function region(text: string, start: string, end: string): string {
  const i = text.indexOf(start);
  expect(i, `找不到起始标记 ${start}`).toBeGreaterThanOrEqual(0);
  const j = text.indexOf(end, i + start.length);
  expect(j, `找不到结束标记 ${end}`).toBeGreaterThanOrEqual(0);
  return text.slice(i, j + end.length);
}

class FakeStorage implements DraftStorageLike {
  private map = new Map<string, string>();
  getItem(key: string): string | null {
    return this.map.has(key) ? this.map.get(key)! : null;
  }
  setItem(key: string, value: string): void {
    this.map.set(key, value);
  }
  removeItem(key: string): void {
    this.map.delete(key);
  }
  has(key: string): boolean {
    return this.map.has(key);
  }
  raw(key: string): string | null {
    return this.map.get(key) ?? null;
  }
}

function rule(over: Partial<ModelRouteRule> = {}): ModelRouteRule {
  return {
    fromSite: 'domestic',
    match: 'gpt-5.6',
    site: 'global',
    target: 'gpt-5.6',
    mode: 'onExhausted',
    note: '',
    ...over,
  };
}

const TITLE: TitleRoute = { enabled: true, domestic: 'free-dom', global: 'free-glo' };

describe('draft storage keys', () => {
  it('key 带版本号，版本号与信封一致', () => {
    expect(ROUTES_DRAFT_KEY).toMatch(/^cbp\.models\.routesDraft\.v\d+$/);
    expect(TITLE_DRAFT_KEY).toMatch(/^cbp\.models\.titleDraft\.v\d+$/);
    expect(ROUTES_DRAFT_KEY.endsWith(`.v${DRAFTS_SCHEMA_VERSION}`)).toBe(true);
    expect(TITLE_DRAFT_KEY.endsWith(`.v${DRAFTS_SCHEMA_VERSION}`)).toBe(true);
    expect(ROUTES_DRAFT_KEY).not.toBe(TITLE_DRAFT_KEY);
  });

  it('没有 window 时退化为纯内存，不抛异常', () => {
    expect(typeof window).toBe('undefined');
    expect(getSessionStorage()).toBeNull();
    // storage 为 null 时所有 helper 都必须是安全的空操作
    expect(readDraft(null, ROUTES_DRAFT_KEY, normalizeRulesDraft)).toBeNull();
    expect(writeDraft(null, ROUTES_DRAFT_KEY, [rule()], 'base')).toBe(false);
    expect(() => clearDraft(null, ROUTES_DRAFT_KEY)).not.toThrow();
  });
});

describe('routes draft round-trip', () => {
  it('写入后能原样读回，信封带版本与基线', () => {
    const storage = new FakeStorage();
    const draft = [rule(), rule({ match: 'glm-5', note: '备用' })];
    expect(writeDraft(storage, ROUTES_DRAFT_KEY, draft, 'BASE_FP', 1234)).toBe(true);

    const record = readDraft(storage, ROUTES_DRAFT_KEY, normalizeRulesDraft);
    expect(record?.draft).toEqual(draft);
    expect(record?.base).toBe('BASE_FP');
    expect(record?.at).toBe(1234);

    const envelope = JSON.parse(storage.raw(ROUTES_DRAFT_KEY)!) as { v: number };
    expect(envelope.v).toBe(DRAFTS_SCHEMA_VERSION);
  });

  it('顺序即优先级，往返后不能变', () => {
    const storage = new FakeStorage();
    const draft = [rule({ match: 'a' }), rule({ match: 'b' }), rule({ match: 'c' })];
    writeDraft(storage, ROUTES_DRAFT_KEY, draft, 'BASE');
    expect(readDraft(storage, ROUTES_DRAFT_KEY, normalizeRulesDraft)?.draft).toEqual(draft);
  });
});

describe('corrupt / drifted stored values are ignored and removed', () => {
  const corruptCases: Array<[string, string]> = [
    ['截断的 JSON', '{"v":1,"base":"x","draft":'],
    ['不是对象', '"just-a-string"'],
    ['数组不是信封', '[]'],
    ['版本不符', JSON.stringify({ v: DRAFTS_SCHEMA_VERSION + 1, base: '', draft: [] })],
    ['缺版本', JSON.stringify({ base: '', draft: [] })],
    ['base 不是字符串', JSON.stringify({ v: DRAFTS_SCHEMA_VERSION, base: 42, draft: [] })],
    ['draft 不是数组', JSON.stringify({ v: DRAFTS_SCHEMA_VERSION, base: '', draft: 'x' })],
    [
      '规则缺目标模型',
      JSON.stringify({
        v: DRAFTS_SCHEMA_VERSION,
        base: '',
        draft: [{ fromSite: 'domestic', match: 'x', site: 'global' }],
      }),
    ],
    [
      '规则项不是对象',
      JSON.stringify({ v: DRAFTS_SCHEMA_VERSION, base: '', draft: ['x'] }),
    ],
  ];

  for (const [label, raw] of corruptCases) {
    it(`${label} → 当没有草稿，并从存储里删掉`, () => {
      const storage = new FakeStorage();
      storage.setItem(ROUTES_DRAFT_KEY, raw);
      expect(readDraft(storage, ROUTES_DRAFT_KEY, normalizeRulesDraft)).toBeNull();
      expect(storage.has(ROUTES_DRAFT_KEY)).toBe(false);
    });
  }

  it('标题草稿的类型漂移同样被拒', () => {
    const storage = new FakeStorage();
    storage.setItem(
      TITLE_DRAFT_KEY,
      JSON.stringify({ v: DRAFTS_SCHEMA_VERSION, base: '', draft: { enabled: 'yes' } }),
    );
    expect(readDraft(storage, TITLE_DRAFT_KEY, normalizeTitleDraft)).toBeNull();
    expect(storage.has(TITLE_DRAFT_KEY)).toBe(false);

    storage.setItem(
      TITLE_DRAFT_KEY,
      JSON.stringify({ v: DRAFTS_SCHEMA_VERSION, base: '', draft: { domestic: 5 } }),
    );
    expect(readDraft(storage, TITLE_DRAFT_KEY, normalizeTitleDraft)).toBeNull();
    expect(storage.has(TITLE_DRAFT_KEY)).toBe(false);
  });

  it('未知字段被丢弃，而不是原样搬到页面状态', () => {
    const drifted = normalizeRulesDraft([
      { ...rule(), evil: 'payload', fromSite: 'CN', mode: 'onexhausted' },
    ]);
    expect(drifted).toEqual([rule({ fromSite: 'domestic', mode: 'onExhausted' })]);
    expect(Object.keys(drifted![0])).not.toContain('evil');

    expect(normalizeTitleDraft({ enabled: true, evil: 1 })).toEqual({
      enabled: true,
      domestic: '',
      global: '',
    });
  });

  it('parseStoredDraft 对空串 / null 直接返回 null', () => {
    expect(parseStoredDraft(null, normalizeRulesDraft)).toBeNull();
    expect(parseStoredDraft('', normalizeRulesDraft)).toBeNull();
  });
});

describe('restore policy（不许让旧草稿盖掉更新的服务端值）', () => {
  const serverA = [rule({ match: 'a' })];
  const edited = [rule({ match: 'b' })];
  const titleServer = titleFromPayload({ enabled: false });
  const titleEdited = titleFromPayload(TITLE);

  it('载荷还没加载（指纹为空）⇒ 不恢复，草稿留在存储里等下次', () => {
    expect(
      shouldRestoreDraft({ draft: edited, base: rulesFingerprint(serverA), at: 0 }, '', rulesFingerprint),
    ).toBe(false);
    expect(
      shouldRestoreDraft(
        { draft: titleEdited, base: titleFingerprint(titleServer), at: 0 },
        '',
        titleFingerprint,
      ),
    ).toBe(false);
  });

  it('服务端已经等于草稿（别人保存了同一份）⇒ 不恢复', () => {
    expect(
      shouldRestoreDraft(
        { draft: edited, base: rulesFingerprint(serverA), at: 0 },
        rulesFingerprint(edited),
        rulesFingerprint,
      ),
    ).toBe(false);
    expect(
      shouldRestoreDraft(
        { draft: titleEdited, base: titleFingerprint(titleServer), at: 0 },
        titleFingerprint(titleEdited),
        titleFingerprint,
      ),
    ).toBe(false);
  });

  it('草稿与写入基线相同（当时没改）⇒ 不恢复', () => {
    expect(
      shouldRestoreDraft(
        { draft: serverA, base: rulesFingerprint(serverA), at: 0 },
        rulesFingerprint(serverA),
        rulesFingerprint,
      ),
    ).toBe(false);
  });

  it('确实有未保存编辑 ⇒ 恢复（页面因此必然显示脏标记）', () => {
    expect(
      shouldRestoreDraft(
        { draft: edited, base: rulesFingerprint(serverA), at: 0 },
        rulesFingerprint(serverA),
        rulesFingerprint,
      ),
    ).toBe(true);
    expect(
      shouldRestoreDraft(
        { draft: titleEdited, base: titleFingerprint(titleServer), at: 0 },
        titleFingerprint(titleServer),
        titleFingerprint,
      ),
    ).toBe(true);
    // 没有基线（旧条目）但确实与当前服务端不同 → 仍然恢复
    expect(
      shouldRestoreDraft({ draft: edited, base: '', at: 0 }, rulesFingerprint(serverA), rulesFingerprint),
    ).toBe(true);
  });

  it('完整链路：切标签 → 离开期间服务端前进 → 回来不复活旧草稿', () => {
    const storage = new FakeStorage();
    writeDraft(storage, ROUTES_DRAFT_KEY, edited, rulesFingerprint(serverA));

    const stored = readDraft(storage, ROUTES_DRAFT_KEY, normalizeRulesDraft);
    // 同一份服务端值：恢复
    expect(shouldRestoreDraft(stored, rulesFingerprint(serverA), rulesFingerprint)).toBe(true);
    // 服务端已推进到草稿的内容：不恢复（并且下一次持久化 effect 会清空它）
    expect(shouldRestoreDraft(stored, rulesFingerprint(edited), rulesFingerprint)).toBe(false);

    clearDraft(storage, ROUTES_DRAFT_KEY);
    expect(storage.has(ROUTES_DRAFT_KEY)).toBe(false);
  });
});

describe('ModelsPage 接线（源码级）', () => {
  it('从 lib/drafts 取 helper，并同时使用两条版本化 key', () => {
    expect(models).toContain("} from '@/lib/drafts';");
    expect(models).toContain('readDraft(draftStorage, ROUTES_DRAFT_KEY, normalizeRulesDraft)');
    expect(models).toContain('readDraft(draftStorage, TITLE_DRAFT_KEY, normalizeTitleDraft)');
    expect(models).toContain('writeDraft(draftStorage, ROUTES_DRAFT_KEY, rules, savedSnapshot)');
    expect(models).toContain('writeDraft(draftStorage, TITLE_DRAFT_KEY, normalized, titleBase)');
    // 只做会话级持久化：不许出现 localStorage / 服务端草稿接口
    expect(models).not.toContain('localStorage');
  });

  it('挂载时 hydrate，且每份草稿只 hydrate 一次', () => {
    const hydrate = region(
      models,
      'pendingRoutesDraft.current = readDraft(',
      '}, [draftStorage]);',
    );
    expect(hydrate).toContain('pendingTitleDraft.current = readDraft(');
    // 读取必须早于 payload effect（同一轮 commit 内 effect 按声明顺序执行）
    expect(models.indexOf('pendingRoutesDraft.current = readDraft(')).toBeLessThan(
      models.indexOf('consumedPayloadRef.current = routesPayload;'),
    );
    expect(models).toContain('if (!routesHydratedRef.current) {');
    expect(models).toContain('if (!titleHydratedRef.current) {');
    expect(models).toContain('routesHydratedRef.current = true;');
    expect(models).toContain('titleHydratedRef.current = true;');
    // 恢复判据必须过 shouldRestoreDraft，而不是「存储里有就直接用」
    expect(models).toContain('shouldRestoreDraft(stored, snap, rulesFingerprint)');
    expect(models).toContain('shouldRestoreDraft(storedTitle, nextTitleSnap, titleFingerprint)');
  });

  it('只持久化脏草稿：与快照一致就清空，载荷未到不写', () => {
    const persistRoutes = region(
      models,
      'if (!routesHydratedRef.current || !draftStorage || !savedSnapshot) return;',
      '}, [rules, savedSnapshot, draftStorage]);',
    );
    expect(persistRoutes).toContain('if (rulesFingerprint(rules) === savedSnapshot) {');
    expect(persistRoutes).toContain('clearDraft(draftStorage, ROUTES_DRAFT_KEY);');

    const persistTitle = region(
      models,
      'if (!titleHydratedRef.current || !draftStorage || !titleBase) return;',
      '}, [titleRoute, titleBase, draftStorage]);',
    );
    expect(persistTitle).toContain('if (titleFingerprint(normalized) === titleBase) {');
    expect(persistTitle).toContain('clearDraft(draftStorage, TITLE_DRAFT_KEY);');
  });

  it('父级载荷更新仍然不覆盖脏草稿（规则 + 标题）', () => {
    expect(models).toContain(
      "setRules((prev) => (prevSnap && JSON.stringify(prev) !== prevSnap ? prev : normalized));",
    );
    expect(models).toContain('prevTitleSnap && JSON.stringify(prev) !== prevTitleSnap ? prev : nextTitle,');
  });

  it('保存路由：2xx 之后才清草稿，catch 分支不清', () => {
    const saveRoutes = region(models, 'const handleSaveRoutes = async () => {', 'const explainRule');
    const postAt = saveRoutes.indexOf("await apiRequest('/direct-admin/api/model-routes'");
    const clearAt = saveRoutes.indexOf('clearDraft(draftStorage, ROUTES_DRAFT_KEY)');
    expect(postAt).toBeGreaterThanOrEqual(0);
    expect(clearAt).toBeGreaterThan(postAt);

    const catchBlock = region(saveRoutes, '} catch (e: unknown) {', '} finally {');
    expect(catchBlock).toContain('草稿已保留');
    expect(catchBlock).not.toContain('clearDraft');
  });

  it('保存标题：2xx 之后才清草稿，catch 分支不清', () => {
    const saveTitle = region(
      models,
      'const handleSaveTitleRoute = async () => {',
      'const handleSaveRoutes = async () => {',
    );
    const postAt = saveTitle.indexOf("await apiRequest('/direct-admin/api/model-routes'");
    const clearAt = saveTitle.indexOf('clearDraft(draftStorage, TITLE_DRAFT_KEY)');
    expect(postAt).toBeGreaterThanOrEqual(0);
    expect(clearAt).toBeGreaterThan(postAt);

    const catchBlock = region(saveTitle, '} catch (e: unknown) {', '} finally {');
    expect(catchBlock).not.toContain('clearDraft');
  });

  it('两条放弃路径都清存储（否则切页回来草稿复活）', () => {
    const resetTitle = region(models, 'const handleResetTitleRoute = () => {', 'const handleSaveTitleRoute');
    expect(resetTitle).toContain('setTitleRoute(titleFromPayload(routesPayload?.titleRoute))');
    expect(resetTitle).toContain('clearDraft(draftStorage, TITLE_DRAFT_KEY)');

    const resetRoutes = region(models, '<DirtySaveBar', 'onSave={handleSaveRoutes}');
    expect(resetRoutes).toContain('clearDraft(draftStorage, ROUTES_DRAFT_KEY)');
  });

  it('脏标记仍然在（路由状态行 + 标题状态文案 + 底部保存条）', () => {
    expect(models).toContain('id="routeSaveState"');
    expect(models).toContain('data-state={routeSaveCode}');
    expect(models).toContain("'有未保存的路由规则'");
    expect(models).toContain("if (titleDirty) titleStateText += ' · 有未保存的更改';");
    expect(models).toContain("titleStateText = '未启用（会话标题按原模型计费）';");
    expect(models).toContain('{titleDirty && (');
    expect(models).toContain('放弃标题草稿');
    expect(models).toContain('visible={isDirty}');
  });
});
