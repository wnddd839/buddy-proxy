// 最小 node 类型声明 —— 只为 `web/src/__tests__/panel-contract.test.ts`
// 的源码级契约测试服务。
//
// 项目（上游 v0.8.0）没有装 `@types/node`，而 vitest 测试要读源码文本。
// 与其为一个测试引入一整个 @types/node（还要改 package.json / lock），
// 这里只声明实际用到的 4 个 API。若日后引入 @types/node，删掉本文件即可。

declare module 'node:fs' {
  export interface Dirent {
    name: string;
    isDirectory(): boolean;
    isFile(): boolean;
  }
  export function readFileSync(path: string, encoding: 'utf8'): string;
  export function readdirSync(path: string): string[];
  export function readdirSync(
    path: string,
    options: { withFileTypes: true },
  ): Dirent[];
}

declare module 'node:path' {
  export function dirname(p: string): string;
  export function join(...parts: string[]): string;
  export function resolve(...parts: string[]): string;
  export function basename(p: string): string;
}

declare module 'node:url' {
  export function fileURLToPath(url: string | URL): string;
}
