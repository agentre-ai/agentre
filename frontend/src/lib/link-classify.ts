export type LinkClass =
  | { kind: "url"; url: string }
  | {
      kind: "local-internal";
      fullPath: string;
      pathKind: LocalPathKind;
      relPath: string;
      line?: number;
      col?: number;
    }
  | {
      kind: "local-external";
      fullPath: string;
      pathKind: LocalPathKind;
      line?: number;
      col?: number;
    }
  | { kind: "unknown"; href: string };

export type LocalPathKind = "file" | "folder";

const URL_PREFIX = /^(https?:|mailto:|tel:)/i;
const WWW_PREFIX = /^www\./i;
const FILE_PROTOCOL = /^file:\/\//i;
const ABS_POSIX = /^\//;
const ABS_WINDOWS = /^[A-Za-z]:[\\/]/;
const LINE_SUFFIX = /:(\d+)(?::(\d+))?$/;

function stripLineSuffix(p: string): {
  path: string;
  line?: number;
  col?: number;
} {
  const m = LINE_SUFFIX.exec(p);
  if (!m) return { path: p };
  return {
    path: p.slice(0, m.index),
    line: parseInt(m[1], 10),
    col: m[2] !== undefined ? parseInt(m[2], 10) : undefined,
  };
}

function fileURLToPath(href: string): string {
  // file:///Users/x/foo.go → /Users/x/foo.go
  // file:///C:/Users/x/foo.go → C:/Users/x/foo.go
  let p = href.slice("file://".length);
  if (p.startsWith("/") && /^[A-Za-z]:/.test(p.slice(1))) {
    p = p.slice(1);
  }
  try {
    return decodeURI(p);
  } catch {
    return p;
  }
}

function classifyLocalPathKind(fullPath: string, cwd?: string): LocalPathKind {
  if (cwd && fullPath === cwd) return "folder";
  if (fullPath.endsWith("/") || /[\\/]$/.test(fullPath)) return "folder";
  return "file";
}

export function classifyLink(
  href: string | undefined,
  cwd?: string,
): LinkClass {
  if (!href) return { kind: "unknown", href: "" };

  if (URL_PREFIX.test(href)) return { kind: "url", url: href };
  if (WWW_PREFIX.test(href)) return { kind: "url", url: `http://${href}` };

  let rawPath: string;
  if (FILE_PROTOCOL.test(href)) {
    rawPath = fileURLToPath(href);
  } else if (ABS_POSIX.test(href) || ABS_WINDOWS.test(href)) {
    rawPath = href;
  } else {
    return { kind: "unknown", href };
  }

  const { path: fullPath, line, col } = stripLineSuffix(rawPath);
  const pathKind = classifyLocalPathKind(fullPath, cwd);

  if (cwd && (fullPath === cwd || fullPath.startsWith(cwd + "/"))) {
    const relPath = fullPath === cwd ? "" : fullPath.slice(cwd.length + 1);
    return {
      kind: "local-internal",
      fullPath,
      pathKind,
      relPath,
      ...(line !== undefined ? { line } : {}),
      ...(col !== undefined ? { col } : {}),
    };
  }

  return {
    kind: "local-external",
    fullPath,
    pathKind,
    ...(line !== undefined ? { line } : {}),
    ...(col !== undefined ? { col } : {}),
  };
}
