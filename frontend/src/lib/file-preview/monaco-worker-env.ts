import editorWorker from "monaco-editor/editor/editor.worker?worker";

// Monaco 需要 self.MonacoEnvironment.getWorker 才能拿到编辑器 worker。
// Vite `?worker` 把每个 worker 打成独立 chunk，跟随应用产物（//go:embed）加载，
// 离线桌面不触网。注意 monaco-editor 0.56 的 exports 映射 `"./*": "./esm/vs/*.js"`
// 已自带 esm/vs/ 前缀 —— 深导入必须写 monaco-editor/editor/editor.worker
// （带 esm/vs/ 前缀会双写路径导致 Rollup 解析失败）。
// 只读预览只需要 editor worker（文本模型 / 词法 / diff 计算）；
// TS/JSON/CSS/HTML 等 language worker 只服务 IntelliSense，预览用不到，不打包。
// 本模块只被 monaco-loader 动态 import，组件 / 测试永不静态触碰。
self.MonacoEnvironment = {
  getWorker() {
    return new editorWorker();
  },
};
