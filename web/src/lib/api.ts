export async function apiRequest<T = unknown>(
  path: string,
  opts: RequestInit = {},
): Promise<T> {
  const headers: Record<string, string> = {
    'content-type': 'application/json',
    ...((opts.headers as Record<string, string>) || {}),
  };
  const res = await fetch(path, {
    ...opts,
    headers,
    credentials: 'same-origin',
  });
  const text = await res.text();
  let data: Record<string, unknown> = {};
  try {
    data = text ? (JSON.parse(text) as Record<string, unknown>) : {};
  } catch {
    data = { raw: text };
  }
  // 200 + {"ok":false} 也是失败：这类响应如果当成功，界面会显示
  // 「已保存」并继续用旧数据，属于本分支最忌讳的静默失效。
  if (data && typeof data === "object" && (data as { ok?: unknown }).ok === false) {
    const err = (data as { error?: unknown }).error;
    throw new Error(typeof err === "string" && err ? err : "请求返回 ok:false");
  }
  if (!res.ok) {
    const errObj = data?.error as { message?: string } | string | undefined;
    const msg =
      (typeof errObj === 'object' && errObj?.message) ||
      (typeof data?.message === 'string' && data.message) ||
      (typeof errObj === 'string' && errObj) ||
      `HTTP ${res.status}`;
    throw new Error(msg);
  }
  return data as T;
}

/**
 * 更新请求单独使用 AbortController 超时控制：
 * 重启交接期间旧连接可能长时间挂起，默认 3 秒用于本地轮询，检查 Release 时放宽至 30 秒。
 */
export async function updateFetch<T = unknown>(
  path: string,
  opts: RequestInit = {},
  timeoutMs = 3000,
): Promise<T> {
  const ctrl = new AbortController();
  const timer = window.setTimeout(() => ctrl.abort(), timeoutMs);
  try {
    const res = await fetch(path, {
      ...opts,
      credentials: 'same-origin',
      signal: ctrl.signal,
    });
    const text = await res.text();
    let data: Record<string, unknown> = {};
    try {
      data = text ? (JSON.parse(text) as Record<string, unknown>) : {};
    } catch {
      data = { raw: text };
    }
    if (!res.ok) {
      const errObj = data?.error as { message?: string } | string | undefined;
      const msg =
        (typeof errObj === 'object' && errObj?.message) ||
        (typeof data?.message === 'string' && data.message) ||
        (typeof errObj === 'string' && errObj) ||
        `HTTP ${res.status}`;
      throw new Error(msg);
    }
    return data as T;
  } catch (e: unknown) {
    if (e instanceof DOMException && e.name === 'AbortError') {
      throw new Error(`请求超时（${Math.round(timeoutMs / 1000)} 秒）`);
    }
    if (e instanceof Error && e.name === 'AbortError') {
      throw new Error(`请求超时（${Math.round(timeoutMs / 1000)} 秒）`);
    }
    throw e;
  } finally {
    window.clearTimeout(timer);
  }
}

export async function copyToClipboard(text: string): Promise<void> {
  const value = String(text || '');
  if (!value) throw new Error('内容为空');
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const ta = document.createElement('textarea');
  ta.value = value;
  ta.style.position = 'fixed';
  ta.style.opacity = '0';
  document.body.appendChild(ta);
  ta.select();
  document.execCommand('copy');
  ta.remove();
}
