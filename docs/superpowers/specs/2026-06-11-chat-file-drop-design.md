# 聊天输入框拖拽文件/文件夹设计 (Drag-and-drop files into chat composer)

- 日期: 2026-06-11
- 范围: `agentre/` (Wails 桌面端,Go 后端 + React 前端)
- 状态: 待实现

## 背景与问题

聊天输入框 (`ChatComposer`, `frontend/src/components/agentre/chat.tsx`) 当前只支持两种附件入口:

1. **粘贴** (`onPasteCapture` → `handlePasteCapture`, chat.tsx:533):从剪贴板提取 `image/*` 文件,读成 base64 dataURL 作为图片附件。
2. **图片选择按钮** (隐藏 `<input type=file>`, chat.tsx:670):同样只收 `image/png,image/jpeg,image/webp`。

**完全没有拖拽支持。** 用户把文件/文件夹拖进窗口时:
- 走 webview 默认行为(可能导航/打开文件),不会进入输入框;
- 即便监听 HTML5 `drop` 事件,**webview 出于安全也只给 `File` 对象,拿不到绝对路径**——这是浏览器/webview 的硬限制。

用户诉求:把文件/文件夹拖进输入框时,
- **非图片文件 → 直接在输入框填入文件绝对路径**;
- **文件夹 → 直接填入文件夹绝对路径**;
- **图片文件 → 仍按现有图片附件方式预览**(与粘贴一致,送给模型做视觉输入)。

## 目标

- 在聊天输入框支持拖拽 **文件 + 文件夹**(可一次拖多个)。
- 图片文件:附件预览(复用现有图片附件链路:缩略图 + base64 内联)。
- 非图片文件 / 文件夹:在光标处插入**绝对路径**文本。
- 拖拽永不"死路":每个拖入项最终要么成为图片预览,要么变成输入框里的路径,**不会被静默丢弃**。

## 非目标 (YAGNI)

- 不做拖拽**排序/重排**附件。
- 不在终端面板、消息记录区等其它区域支持文件拖拽(本期只 composer)。
- 不支持把图片以外的文件做"内容内联"(只给路径,不读内容塞进消息;编码 Agent 本身有文件系统访问能力,可自行 Read)。
- 不改 `SendRequest` / 后端消息协议(图片仍走现有 `Images []SendImage`,路径只是纯文本)。
- 不做拖拽进度条 / 大文件分片(图片 ≤ 5MB,沿用现有上限)。

## 关键技术结论(为什么必须用 Wails 原生拖拽)

| 机制 | 给绝对路径? | 给文件内容? | 结论 |
| ---- | ----------- | ----------- | ---- |
| webview HTML5 `drop` (`dataTransfer.files`) | ❌(安全限制) | ✅(`File` 对象) | 拿不到路径,**不可用** |
| Wails `OnFileDrop` 原生拖拽 (`options.DragAndDrop{EnableFileDrop:true}`) | ✅(`paths []string`) | ❌(只给路径) | **唯一能拿到绝对路径的途径** |

Wails v2.12.0(本仓库当前版本)已支持。运行时 JS API 已存在于
`frontend/wailsjs/runtime/runtime.d.ts:239`:

```ts
export function OnFileDrop(
  callback: (x: number, y: number, paths: string[]) => void,
  useDropTarget: boolean,
): void
export function OnFileDropOff(): void
```

因为 `OnFileDrop` 只给路径不给内容,**图片要做附件预览必须由后端按路径读取文件**(前端无法读任意 fs 路径)。这是本设计需要新增后端绑定的根因。

## 关键决策(已与用户确认)

| 决策点 | 选择 |
| ------ | ---- |
| 图片拖入行为 | **附件预览**(复用现有图片附件链路;不是只插路径) |
| 文件夹拖入行为 | 插入绝对路径(与非图片文件同一分支) |
| 拖放目标范围 | **仅 composer**,不是整窗口 |
| 路径插入位置 | **光标处**插入,多个路径以空格分隔、末尾补一个空格 |
| 含空格的路径 | 用双引号包裹(`"/Users/a b/x.txt"`) |
| 拿不到内容/超限/类型不符的图片 | **降级为插入路径**(见下"统一降级原则") |

### 统一降级原则(拖拽永不死路)

拖拽只有两种结局:**图片预览** 或 **插入路径**。任何"无法成为图片附件"的情况都降级为插入路径,而**不弹错误**:

- 文件夹、非图片文件 → 路径;
- 图片但超过 5MB → 路径(`os.Stat` 先判大小,**不读取**大文件);
- 扩展名像图片但实际是目录 / 嗅探出的 MIME 不在 `png/jpeg/webp` → 路径;
- 文件读取失败 / 不存在 → 路径(让 Agent 自己去试)。

> 这与**粘贴**链路的行为不同(粘贴对超限/类型不符会用 `imageError` 报错)。这是有意的:拖拽强调"东西总能进到框里",而粘贴是图片专用入口。`imageError` 槽位保持给粘贴/选择按钮使用,拖拽链路不写它。

### 图片数量上限的处理

现有上限 `MAX_CHAT_IMAGE_COUNT = 4`。一次拖入多张图片时:
- 按现有附件数 + 本次图片数,**能装下的装为附件**;
- **超出 4 张配额的图片,降级为插入路径**(符合统一降级原则,不静默丢)。

## 设计

### 整体数据流

```
用户拖入文件/文件夹
   │  (OS 级 drop → Wails 原生)
   ▼
OnFileDrop(x, y, paths)            ← 全局单一回调 (lib/file-drop.ts)
   │  document.elementFromPoint(x,y) 命中哪个已注册 drop zone
   ▼
ChatComposer 的 onPaths(paths)
   │  classifyDroppedPaths(paths)  ← 纯函数,按扩展名分流
   ├── plainPaths (非图片扩展名: 普通文件 + 文件夹)
   │        └─► insertPathsIntoEditor → AIChatInput.insertText()
   └── imageCandidates (.png/.jpg/.jpeg/.webp)
            └─► ChatReadDroppedImages(paths)   ← 后端按路径读+校验+stat
                   ├── kind="image" → 追加为图片附件 (配额内)
                   ├── kind="path"  → 降级,插入路径
                   └── 配额溢出     → 降级,插入路径
```

### A. 启用 Wails 原生文件拖拽 (`main.go`)

在 `newWailsOptionsForDataDir` 的 `options.App` 上新增:

```go
DragAndDrop: &options.DragAndDrop{
    EnableFileDrop: true,
    // DisableWebViewDrop 保持 false:让 webview 仍能收到 HTML5 dragenter/leave
    // 事件以驱动拖拽高亮;落地的真实路径只来自原生 OnFileDrop。
    // CSSDropProperty/CSSDropValue 用默认值 "--wails-drop-target" / "drop"。
},
```

> `main.go` 当前无 `main_test.go`,这一行属于 Wails 装配(I/O 边界),靠手动验证;真正的逻辑都在下面可单测的纯函数 / 后端方法里。

### B. 前端拖拽路由层 (`frontend/src/lib/file-drop.ts`,新增)

`OnFileDrop` 是**每窗口唯一**的全局回调,而界面里可能同时挂多个 `ChatComposer`(普通会话 / 群聊)。因此用一个集中式注册表 + `elementFromPoint` 把 drop 路由到光标所在的 composer:

```ts
type DropHandler = (paths: string[]) => void;

// 注册一个 drop zone;首次注册时挂上全局 OnFileDrop,最后一个注销时 OnFileDropOff。
// 返回注销函数。注册时给 el 打上 CSS 标记 --wails-drop-target: drop,
// 使 Wails 的 useDropTarget=true 只在 composer 上空触发。
export function registerDropZone(el: HTMLElement, handler: DropHandler): () => void;

// 仅测试用:重置注册表(清掉 module 级单例状态)。
export function __resetDropRegistryForTest(): void;
```

内部:

```ts
OnFileDrop((x, y, paths) => {
  const target = document.elementFromPoint(x, y);
  // 从 target 向上找最近的已注册 zone(zone 是注册表里的 key 元素或其祖先)
  for (const [el, handler] of registry) {
    if (el === target || el.contains(target)) { handler(paths); return; }
  }
  // 没命中任何 composer → 忽略
}, /* useDropTarget */ true);
```

- `useDropTarget=true`:Wails 只在带 `--wails-drop-target: drop` 的元素上空触发,天然把范围收敛到 composer。
- `elementFromPoint(x,y)` 用 Wails 给的视口坐标(CSS px),与浏览器坐标系一致(实现时验证)。

### C. `useFileDropZone` hook (`frontend/src/components/agentre/chat-input/use-file-drop.ts`,新增)

封装注册 + 视觉反馈,供 `ChatComposer` 使用:

```ts
function useFileDropZone(opts: {
  ref: React.RefObject<HTMLElement | null>;
  enabled: boolean;
  onPaths: (paths: string[]) => void;
}): { isDragOver: boolean };
```

- mount 且 `enabled` 时 `registerDropZone(ref.current, onPaths)`,卸载时注销。
- 在 zone 元素上挂 `dragenter/dragover/dragleave/drop` 监听:
  - `dragover`/`drop` 调 `preventDefault()`——**阻止 webview 把拖入文件当导航/打开**;
  - `dragenter`/`dragleave` 维护 `isDragOver`(进入计数法避免子元素抖动)。
- `isDragOver` 纯属视觉:真正落地走原生 `OnFileDrop`,即便某平台不发 HTML5 拖拽事件,功能照常工作,只是没有高亮。

### D. 分类与路径格式化(纯函数,`frontend/src/components/agentre/chat-input/drop.ts`,新增)

```ts
const DROP_IMAGE_EXTENSIONS = ["png", "jpg", "jpeg", "webp"]; // 对齐 CHAT_IMAGE_ACCEPT

// 按扩展名(小写、取最后一段)分流。文件夹通常无图片扩展名 → 落入 plainPaths。
export function classifyDroppedPaths(paths: string[]): {
  imageCandidates: string[];
  plainPaths: string[];
};

// 把若干绝对路径拼成插入文本:含空白的路径用双引号包裹,空格分隔,末尾补一个空格。
//   formatPathsForInput(["/a/b.txt", "/c d/e"]) === `/a/b.txt "/c d/e" `
export function formatPathsForInput(paths: string[]): string;
```

### E. `AIChatInput` 新增 `insertText`

`AIChatInputHandle` (`chat-input/types.ts`) 加一个方法,与现有 slash `literal_text` 插入(index.tsx:289)同一手法:

```ts
export interface AIChatInputHandle {
  // …现有 focus/clear/isEmpty/submit/loadDraft…
  insertText: (text: string) => void; // editor.chain().focus().insertContent(text).run()
}
```

实现挂在 `useImperativeHandle` 里(index.tsx:258)。空 editor 安全 no-op。

### F. `ChatComposer` 集成 (`chat.tsx`)

1. 给 composer 最外层 `<form>`(或其内层卡片 `div`)加 `ref`,接入 `useFileDropZone`:
   ```ts
   const dropRef = React.useRef<HTMLFormElement>(null);
   const { isDragOver } = useFileDropZone({
     ref: dropRef,
     enabled: true, // 恒注册:编辑态也允许插路径,见下方"编辑态"说明
     onPaths: handleDroppedPaths,
   });
   ```
2. `handleDroppedPaths(paths)`:
   ```
   const { imageCandidates, plainPaths } = classifyDroppedPaths(paths);
   const toInsert = [...plainPaths];

   if (imageCandidates.length && supportsImageInput && !editing) {
     const res = await ChatReadDroppedImages({ paths: imageCandidates });
     for (const item of res.items) {
       if (item.kind === "image" && 配额未满) appendImageAttachment(item.image);
       else toInsert.push(item.path);   // kind="path" 或配额溢出 → 降级
     }
   } else {
     // 不支持图片 / 编辑态 → 图片也降级为路径
     toInsert.push(...imageCandidates);
   }

   if (toInsert.length) inputRef.current?.insertText(formatPathsForInput(toInsert));
   ```
3. **重构(范围内)**:把现有 `handleImageFiles` 里"数量上限校验 + 追加 `images` state"抽成一个 `appendImageAttachments(attachments, { onOverflowPaths })` 复用——粘贴/选择按钮(从 `File` 读出的 attachment)与拖拽(从后端读出的 attachment)共用同一追加逻辑,避免两处各写一份配额判断。
4. **拖拽高亮**:`isDragOver` 为真时,在卡片上覆盖一层半透明虚线边框 overlay(文案走 i18n,如"松开以添加文件")。

> **编辑态**:编辑模式下图片附件本就禁用(chat.tsx:462)。拖拽在编辑态仍允许**插入路径**(对修改消息有用),图片也降级为路径。`enabled` 因此恒为 `true`,分支里靠 `!editing && supportsImageInput` 决定走附件还是降级。

### G. 后端:`ChatReadDroppedImages` 按路径读图

**绑定层** (`internal/app/chat.go`,只做透传):
```go
// ChatReadDroppedImages 按绝对路径读取拖入的图片候选,做 stat/类型/大小校验后
// 返回每个路径的归类(image=可附件 / path=降级为纯路径)。不读取 >5MB 的文件。
func (a *App) ChatReadDroppedImages(req *chat_svc.ReadDroppedImagesRequest) (*chat_svc.ReadDroppedImagesResponse, error) {
    return chat_svc.Chat().ReadDroppedImages(a.ctx, req)
}
```

**类型** (`internal/service/chat_svc/types.go`):
```go
type ReadDroppedImagesRequest struct {
    Paths []string `json:"paths"`
}
type ReadDroppedImagesResponse struct {
    Items []DroppedImageItem `json:"items"`
}
type DroppedImageItem struct {
    Path  string     `json:"path"`
    Kind  string     `json:"kind"`            // "image" | "path"
    Image *SendImage `json:"image,omitempty"` // Kind=="image" 时给出 (复用现有 SendImage{Name,DataURL})
}
```

**服务层逻辑** (`chat_svc`,每个 path 独立判定;**永不返回 per-item 错误,最坏降级为 path**):
1. `os.Stat(path)`:出错 / 是目录 / 非常规文件 → `Kind="path"`。
2. `info.Size() > 5MB` → `Kind="path"`(不读)。
3. 读取文件字节,嗅探 MIME(`http.DetectContentType` 取前 512B;webp 检测以实测为准,必要时结合扩展名兜底)。MIME ∉ {image/png,image/jpeg,image/webp} → `Kind="path"`。
4. 否则 `Kind="image"`,`Image = {Name: filepath.Base, DataURL: "data:"+mime+";base64,"+enc}`。

> 大小/类型这套校验与 `agent_svc.validateAvatarDataURL`(agent.go:244)思路一致,但后者校验的是**前端已生成的 dataURL**、且不做 MIME 嗅探;此处是**按路径读字节 + 嗅探**,语义不同,故在 `chat_svc` 内新写一个小的纯函数 `classifyImageBytes(name, mime, size) (DroppedImageItem)`,**不复用** avatar 那条。常量 `maxDropImageBytes = 5*1024*1024` 与前端 `MAX_CHAT_IMAGE_BYTES` 对齐。

## 测试计划 (TDD,Red → Green → Refactor)

**前端纯函数 (`drop.test.ts`)**
- `classifyDroppedPaths`:大小写扩展名 / 无扩展名(文件夹)/ 多段点(`a.tar.png`)/ 混合一批。
- `formatPathsForInput`:含空格加引号 / 多路径空格分隔 / 末尾空格 / 空数组。

**路由层 (`file-drop.test.ts`)**
- mock `OnFileDrop`/`OnFileDropOff`,断言首注册装回调、末注销卸回调。
- mock `document.elementFromPoint` 命中/未命中,断言路由到正确 handler / 无命中不抛。

**`useFileDropZone` / `AIChatInput.insertText`**
- `insertText` 把文本插入 editor(已有 `editorRef` 测试钩子,index.tsx:55)。
- 组件测:dragenter/leave 切换 `isDragOver`;dragover/drop `preventDefault` 被调用。

**`ChatComposer` 组件测 (mock `ChatReadDroppedImages` + runtime)**
- 拖入非图片 → 调 `insertText` 且文本为绝对路径。
- 拖入文件夹路径 → 插入路径。
- 拖入图片(后端返回 image)→ 出现缩略图预览,**不**插入路径。
- 后端把图片判成 `path`(目录/超限)→ 降级插入路径。
- 一次拖 5 张图(配额 4)→ 4 张附件 + 第 5 张路径。
- `supportsImageInput=false` / 编辑态 → 图片也插入路径。

**后端 (`chat_svc` 单测,用临时文件,不连 DB)**
- 合法 png/jpeg/webp(小)→ `Kind="image"`,DataURL 前缀正确。
- 目录路径 → `Kind="path"`。
- > 5MB 文件(造一个稀疏/填充文件)→ `Kind="path"` 且**未读全量**。
- 扩展名 .png 实为文本 → 嗅探不符 → `Kind="path"`。
- 不存在的路径 → `Kind="path"`(不报错)。
- 混合多路径一次请求 → items 顺序与入参对应。

> 绑定层 `ChatReadDroppedImages` 与 `main.go` 的 `DragAndDrop` 装配属 I/O 边界,靠手动验证(拖真实文件/文件夹/图片各一次)。

## i18n

新增可见文案(`frontend/src/i18n/locales/{zh-CN,en}/common.json`):
- 拖拽高亮 overlay 文案,如 `chat.composer.dropHint`:"松开以添加文件" / "Drop files to add"。

(路径文本本身是动态内容,不进 `t(...)`。)

## 实现顺序 / 文件清单

1. **后端**(可独立提交):`types.go` 新类型 + `chat_svc` `ReadDroppedImages` + `classifyImageBytes` + 单测;`app/chat.go` 绑定;`make generate` 刷新 wailsjs 绑定。
2. **前端纯函数**:`chat-input/drop.ts` + `drop.test.ts`。
3. **路由 + hook**:`lib/file-drop.ts` + `chat-input/use-file-drop.ts` + 测试;`AIChatInput.insertText`。
4. **集成**:`ChatComposer` 接 `useFileDropZone` + `handleDroppedPaths` + 重构 `appendImageAttachments` + overlay + i18n。
5. **装配**:`main.go` 启用 `DragAndDrop`。
6. **手动验证**:拖文件 / 文件夹 / 图片 / 多选 / 含空格路径各一次。

涉及文件:
- `agentre/main.go`
- `agentre/internal/app/chat.go`
- `agentre/internal/service/chat_svc/types.go` + 服务实现 + 单测
- `agentre/frontend/src/lib/file-drop.ts` (+test)
- `agentre/frontend/src/components/agentre/chat-input/{types.ts,index.tsx,drop.ts,use-file-drop.ts}` (+tests)
- `agentre/frontend/src/components/agentre/chat.tsx`
- `agentre/frontend/src/i18n/locales/{zh-CN,en}/common.json`
- `agentre/frontend/wailsjs/*`(`make generate` 生成物)
```
