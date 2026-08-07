/**
 * 会话文件预览的可预览性判定与「路径 → 会话级 relPath」换算（spec「预览面板与入口」）。
 *
 * 「变动」模式的行路径可能来自工具调用的绝对路径：只有解析后落在会话 cwd 内的
 * 文件才出预览按钮（越出 cwd 的一律不出）；相对路径原样当 relPath。「目录 / Git」
 * 模式的路径本就相对 cwd，直接可用。任何不在扩展名 allowlist 内的文件不出预览按钮。
 */ export type PreviewKind = "markdown" | "code" | "image";

/** markdown 档只收 .md / .markdown；.mdx 含 JSX、GFM 渲染会碎，明确不入档。 */
const MARKDOWN_EXTENSIONS = new Set([".md", ".markdown"]);

const IMAGE_EXTENSIONS = new Set([
  ".png",
  ".jpg",
  ".jpeg",
  ".gif",
  ".webp",
  ".avif",
  ".bmp",
  ".ico",
  ".svg",
]);

// 代码 / 文本 allowlist —— 与 lib/file-preview/monaco-language 的语言表对齐
// （能高亮就给预览按钮），另加常见纯文本格式；markdown / 图片 / .mdx 不在这里。
const CODE_TEXT_EXTENSIONS = new Set([
  // web 前端 / 标记
  ".js",
  ".jsx",
  ".mjs",
  ".cjs",
  ".ts",
  ".tsx",
  ".mts",
  ".cts",
  ".json",
  ".jsonc",
  ".html",
  ".htm",
  ".xml",
  ".css",
  ".scss",
  ".sass",
  ".less",
  // 文本格式 / 配置
  ".yaml",
  ".yml",
  ".toml",
  ".ini",
  ".cfg",
  ".conf",
  ".txt",
  ".log",
  ".csv",
  // shell / 脚本
  ".sh",
  ".bash",
  ".zsh",
  ".py",
  ".pyw",
  ".rb",
  ".php",
  ".lua",
  ".r",
  ".pl",
  ".ps1",
  ".psd1",
  ".psm1",
  ".bat",
  ".cmd",
  // 系统 / 编译型
  ".c",
  ".h",
  ".cpp",
  ".cc",
  ".cxx",
  ".hpp",
  ".hh",
  ".cs",
  ".java",
  ".go",
  ".rs",
  ".swift",
  ".kt",
  ".kts",
  ".dart",
  ".scala",
  ".sc",
  ".clj",
  ".cljs",
  ".cljc",
  ".groovy",
  ".gradle",
  ".sql",
]);

function extensionOf(path: string): string {
  const base = path.split("/").pop() ?? "";
  const slash = base.lastIndexOf("\\");
  const name = slash >= 0 ? base.slice(slash + 1) : base;
  const dot = name.lastIndexOf(".");
  if (dot <= 0) return "";
  return name.slice(dot).toLowerCase();
}

/** 按扩展名判定文件的可预览类型；不在 allowlist 内返回 null。 */
export function previewKind(path: string): PreviewKind | null {
  const ext = extensionOf(path);
  if (MARKDOWN_EXTENSIONS.has(ext)) return "markdown";
  if (IMAGE_EXTENSIONS.has(ext)) return "image";
  if (CODE_TEXT_EXTENSIONS.has(ext)) return "code";
  return null;
}

const ABS_POSIX = /^\//;
const ABS_WINDOWS = /^[A-Za-z]:[\\/]/;

/**
 * 把文件面板一条路径解析成可交给后端 ReadFile/GitFileContent 的会话级 relPath。
 * 返回 null 表示该文件不出预览按钮（扩展名不在 allowlist，或绝对路径越出 cwd）。
 * 相对路径（目录 / Git 模式）原样通过。
 */
export function resolvePreviewRelPath(
  path: string,
  cwd: string,
): string | null {
  if (previewKind(path) === null) return null;
  if (!ABS_POSIX.test(path) && !ABS_WINDOWS.test(path)) return path;
  if (cwd === "") return null;
  const sep = cwd.includes("\\") ? "\\" : "/";
  const prefix = cwd.endsWith("/") || cwd.endsWith("\\") ? cwd : `${cwd}${sep}`;
  if (path === cwd || !path.startsWith(prefix)) return null;
  return path.slice(prefix.length);
}
