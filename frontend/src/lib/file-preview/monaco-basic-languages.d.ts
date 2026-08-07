// monaco-editor 只为其公共 API（editor.api 等）提供类型；basic-languages 的
// monaco.contribution 是纯侧效应注册模块（全部内置语言的词法 tokenizer），
// 包内无 .d.ts。这里声明为空模块即可，只用于 `await import(...)` 触发注册。
declare module "monaco-editor/basic-languages/monaco.contribution";
