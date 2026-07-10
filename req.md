# 悬浮翻译器需求设计文档

## 1. 项目名称

**floating-translator**

中文名称：**悬浮翻译器**

## 2. 项目背景

在阅读英文论文、网页、文档、软件说明或开发资料时，用户经常需要快速复制英文内容并查看中文翻译。传统翻译软件通常需要用户手动粘贴、点击翻译或切换窗口，操作成本较高。

本项目拟开发一个悬浮桌面翻译器；当前 MVP 以 Windows 为首个支持平台，实现：

* 自动监听 Windows 剪切板变化；
* 当用户复制英文文本后，自动发送给后端 LLM；
* 后端使用 Golang 开发，并基于 Eino 框架调用大语言模型完成翻译；
* 前端以透明悬浮字幕框的形式，将中文翻译结果显示在屏幕中间偏下位置；
* 整个过程无需用户点击、粘贴或额外交互。

最终效果类似电影字幕：用户复制英文后，中文译文自动浮现在屏幕下方。

---

## 3. 项目目标

### 3.1 核心目标

开发一个轻量级悬浮翻译器，实现剪切板英文内容自动翻译和字幕式展示。当前 MVP 运行于 Windows 平台，后续可扩展其他平台。

### 3.2 功能目标

1. 自动监听 Windows 剪切板文本变化；
2. 判断剪切板内容是否需要翻译；
3. 将英文文本发送至 Golang 后端；
4. 后端通过 Eino 框架调用 LLM；
5. 将英文准确、自然地翻译为中文；
6. 前端以透明悬浮字幕框显示翻译结果；
7. 显示过程无需用户交互；
8. 支持自动淡入、停留、淡出；
9. 支持重复内容过滤，避免频繁请求 LLM；
10. 支持基础配置和错误日志。

---

## 4. 使用场景

### 4.1 英文论文阅读

用户在 PDF、浏览器或 Word 中复制英文句子，系统自动翻译为中文，并在屏幕底部显示。

### 4.2 英文网页阅读

用户浏览英文网页时，复制段落后，中文译文自动以字幕形式浮现，不需要切换翻译软件。

### 4.3 英文软件文档阅读

开发者阅读英文 API 文档、开源项目 README 或技术博客时，复制内容即可看到中文解释。

### 4.4 日常英文资料阅读

适用于英文邮件、说明书、网页文章、产品文档等内容的快速理解。

---

## 5. 总体功能描述

程序启动后，在后台运行并监听 Windows 剪切板。

当剪切板中的文本内容发生变化时，程序会执行以下流程：

1. 获取剪切板文本；
2. 判断文本是否为空；
3. 判断文本是否与上一次内容重复；
4. 判断文本是否主要为英文；
5. 判断文本是否为 URL、代码或无效内容；
6. 符合条件后发送给后端翻译服务；
7. 后端调用 Eino + LLM 完成英文到中文翻译；
8. 前端接收翻译结果；
9. 以透明字幕框形式显示在屏幕中间偏下位置；
10. 显示数秒后自动淡出。

---

## 6. 技术栈选型

## 6.1 后端

| 模块       | 技术                                                    |
| -------- | ----------------------------------------------------- |
| 开发语言     | Golang                                                |
| LLM 编排框架 | Eino                                                  |
| 剪切板监听    | Windows API / Go Windows 调用                           |
| 配置管理     | YAML / JSON / ENV                                     |
| 日志       | slog / zap / zerolog                                  |
| 模型调用     | OpenAI-compatible API / DeepSeek / 通义 / 火山方舟 / Ollama |
| 桌面集成     | Wails 后端能力                                            |

## 6.2 前端

| 模块   | 技术                         |
| ---- | -------------------------- |
| 桌面框架 | Wails                      |
| 前端框架 | Vue 3 / React / Svelte     |
| 样式   | CSS / Tailwind CSS         |
| 字幕动画 | CSS transition / animation |
| 窗口样式 | 透明、无边框、始终置顶                |

## 6.3 推荐方案

推荐使用：

```text
Golang + Eino + Wails + Vue 3
```

理由：

* Golang 适合开发 Windows 桌面后台服务；
* Eino 适合构建 LLM 应用链路；
* Wails 能较好地整合 Go 后端和 Web 前端；
* Vue 3 足够轻量，适合开发字幕悬浮 UI；
* 整体打包后可以形成独立 Windows 桌面应用。

---

## 7. 系统架构

```text
┌──────────────────────────────┐
│          Windows 系统          │
│                              │
│  用户复制英文文本到剪切板       │
└───────────────┬──────────────┘
                │
                ▼
┌──────────────────────────────┐
│        剪切板监听模块          │
│                              │
│  监听 WM_CLIPBOARDUPDATE      │
│  获取剪切板纯文本              │
└───────────────┬──────────────┘
                │
                ▼
┌──────────────────────────────┐
│        文本过滤模块            │
│                              │
│  空内容过滤                    │
│  重复内容过滤                  │
│  中文内容过滤                  │
│  URL / 代码过滤                │
│  长文本限制                    │
└───────────────┬──────────────┘
                │
                ▼
┌──────────────────────────────┐
│        翻译服务模块            │
│                              │
│  Golang + Eino                │
│  Prompt Template              │
│  ChatModel                    │
│  LLM API                      │
└───────────────┬──────────────┘
                │
                ▼
┌──────────────────────────────┐
│        前端事件通信            │
│                              │
│  后端推送翻译结果              │
│  前端接收字幕文本              │
└───────────────┬──────────────┘
                │
                ▼
┌──────────────────────────────┐
│        透明悬浮字幕窗口        │
│                              │
│  屏幕中间偏下                  │
│  半透明背景                    │
│  中文字幕                      │
│  自动淡入淡出                  │
└──────────────────────────────┘
```

---

## 8. 功能需求

## 8.1 剪切板监听

### 8.1.1 功能描述

程序启动后，应自动监听 Windows 剪切板变化。当剪切板中的文本内容更新时，自动读取剪切板文本。

### 8.1.2 监听范围

仅监听纯文本内容。

不处理以下类型：

* 图片；
* 文件；
* HTML；
* 富文本；
* 表格；
* 二进制内容。

### 8.1.3 触发条件

当剪切板文本内容变化时触发翻译流程。

### 8.1.4 防抖机制

为避免剪切板短时间内多次变化导致重复请求，应设置防抖机制。

推荐参数：

```text
clipboard_debounce_ms: 300
```

即剪切板变化后延迟 300ms 再读取最终内容。

---

## 8.2 文本过滤

### 8.2.1 空内容过滤

以下内容不触发翻译：

```text
""
" "
"\n"
"\t"
```

### 8.2.2 重复内容过滤

如果本次剪切板文本与上一次已处理文本完全一致，则不重复翻译。

示例：

```text
lastText == currentText
```

则跳过。

### 8.2.3 中文内容过滤

如果文本主要为中文，则不翻译。

判断规则建议：

* 中文字符比例超过 30%，则认为不是纯英文待翻译内容；
* 或英文字符比例低于 50%，则跳过。

### 8.2.4 英文内容判断

当文本中英文字母占比较高时，认为需要翻译。

推荐判断规则：

```text
英文字符数量 / 有效字符数量 >= 0.5
```

### 8.2.5 URL 过滤

如果剪切板内容是 URL，则默认跳过。

示例：

```text
https://example.com
http://example.com
www.example.com
```

### 8.2.6 代码内容过滤

如果文本明显为代码片段，默认跳过。

可根据以下特征判断：

```text
func main()
package main
import (
const xxx =
let xxx =
var xxx =
class Xxx
public static void
#include
```

如果后续需要支持“代码注释翻译”，可通过配置项开启。

### 8.2.7 最大长度限制

为避免复制大段文本导致请求过慢或费用过高，应设置最大字符数。

推荐默认值：

```text
max_text_length: 3000
```

超过该长度时可选择：

1. 直接跳过；
2. 截断前 3000 字符进行翻译；
3. 在日志中记录超长文本。

MVP 阶段建议直接跳过。

---

## 8.3 后端翻译服务

### 8.3.1 功能描述

后端接收需要翻译的英文文本，调用 Eino 框架构建 LLM 请求，返回中文译文。

### 8.3.2 翻译方向

默认翻译方向：

```text
英文 → 简体中文
```

### 8.3.3 翻译要求

翻译结果应满足：

* 准确；
* 自然；
* 符合中文表达习惯；
* 不额外解释；
* 不输出“以下是翻译”等无关内容；
* 尽量保留原文含义；
* 专有名词可保留英文或采用常见译法；
* 技术术语翻译应准确。

### 8.3.4 推荐 Prompt

```text
你是一个专业的英文到中文翻译助手。

请将用户提供的英文内容翻译为自然、准确、流畅的简体中文。

要求：
1. 只输出中文译文；
2. 不要解释；
3. 不要总结；
4. 不要添加原文不存在的信息；
5. 保留必要的英文专有名词；
6. 技术术语应尽量采用常见中文译法。

待翻译内容：
{{input}}
```

### 8.3.5 Eino 调用方式

后端建议使用 Eino 的基础链路：

```text
Prompt Template → ChatModel → Output Parser
```

MVP 阶段不需要复杂 Agent，也不需要多轮记忆。

### 8.3.6 LLM 模型配置

支持通过配置文件选择模型。

示例：

```yaml
llm:
  provider: "openai_compatible"
  base_url: "https://api.example.com/v1"
  api_key: "${LLM_API_KEY}"
  model: "deepseek-chat"
  temperature: 0.2
  timeout_seconds: 20
```

### 8.3.7 温度参数

翻译任务建议使用较低温度：

```text
temperature: 0.1 - 0.3
```

默认推荐：

```text
temperature: 0.2
```

### 8.3.8 超时处理

单次翻译请求应设置超时时间。

推荐默认值：

```text
timeout_seconds: 20
```

超时后：

* 不弹窗；
* 不显示错误字幕；
* 仅写入日志；
* 程序继续监听剪切板。

---

## 8.4 前端悬浮字幕

### 8.4.1 功能描述

前端负责接收后端翻译结果，并以电影字幕形式显示在屏幕中间偏下位置。

### 8.4.2 窗口样式

字幕窗口应满足：

* 无边框；
* 背景透明；
* 始终置顶；
* 不显示任务栏图标；
* 不主动获取焦点；
* 尽量不影响用户当前操作；
* 可选支持鼠标穿透。

### 8.4.3 字幕位置

默认位置：

```text
屏幕水平居中
屏幕垂直方向 75% - 85% 位置
```

也可以理解为：

```text
屏幕中间偏下
类似电影字幕区域
```

### 8.4.4 字幕样式

推荐默认样式：

```css
.subtitle-box {
  max-width: 70vw;
  padding: 14px 24px;
  border-radius: 12px;
  background: rgba(0, 0, 0, 0.38);
  color: #ffffff;
  font-size: 28px;
  line-height: 1.6;
  font-weight: 500;
  text-align: center;
  text-shadow: 0 2px 8px rgba(0, 0, 0, 0.8);
  backdrop-filter: blur(4px);
}
```

### 8.4.5 字幕动画

字幕显示流程：

```text
接收翻译结果
→ 淡入 200ms
→ 停留 6000ms
→ 淡出 800ms
→ 隐藏
```

推荐配置：

```yaml
subtitle:
  fade_in_ms: 200
  display_ms: 6000
  fade_out_ms: 800
```

### 8.4.6 新字幕覆盖规则

当新的翻译结果到来时：

* 立即取消旧字幕隐藏计时器；
* 用新译文覆盖旧译文；
* 重新开始显示计时；
* 重新执行淡入、停留、淡出流程。

### 8.4.7 字幕换行

当译文过长时自动换行。

推荐规则：

```text
最大宽度：屏幕宽度的 70%
最多显示：4 行
超出部分可缩小字号或滚动显示
```

MVP 阶段建议：

```text
最多显示 4 行，超出内容省略
```

---

## 8.5 系统托盘

### 8.5.1 是否需要

MVP 可以不做系统托盘，但正式版本建议增加。

### 8.5.2 托盘功能

系统托盘菜单建议包含：

```text
启用监听
暂停监听
重新加载配置
打开日志目录
退出程序
```

### 8.5.3 默认行为

程序启动后默认启用监听。

---

## 8.6 配置文件

### 8.6.1 配置文件位置

推荐配置文件名：

```text
config.yaml
```

推荐路径：

```text
程序目录/config.yaml
```

或：

```text
%APPDATA%/FloatingTranslator/config.yaml
```

### 8.6.2 配置示例

```yaml
app:
  name: "悬浮翻译器"
  auto_start: false
  log_level: "info"

clipboard:
  enable: true
  debounce_ms: 300
  max_text_length: 3000
  skip_duplicate: true
  skip_url: true
  skip_code: true
  only_translate_english: true

llm:
  provider: "openai_compatible"
  base_url: "https://api.example.com/v1"
  api_key: "${LLM_API_KEY}"
  model: "deepseek-chat"
  temperature: 0.2
  timeout_seconds: 20

subtitle:
  always_on_top: true
  click_through: true
  screen_position: "bottom_center"
  width_percent: 70
  font_size: 28
  max_lines: 4
  background_opacity: 0.38
  fade_in_ms: 200
  display_ms: 6000
  fade_out_ms: 800
```

---

## 9. 非功能需求

## 9.1 性能要求

1. 剪切板监听应保持低资源占用；
2. 程序空闲时 CPU 占用应接近 0；
3. 翻译请求不应阻塞 UI；
4. 字幕显示应流畅；
5. 程序长时间运行不应出现内存明显增长。

## 9.2 稳定性要求

1. LLM 请求失败不应导致程序崩溃；
2. 网络异常不应导致程序退出；
3. 剪切板读取失败时应自动恢复；
4. 前端窗口异常时应可重新创建；
5. 后端 panic 应有日志记录。

## 9.3 安全性要求

1. API Key 不应硬编码到源码中；
2. 配置文件不应提交到公开仓库；
3. 日志不应记录完整敏感文本；
4. 可配置是否记录原文；
5. 默认只记录错误信息和文本长度。

## 9.4 隐私要求

由于剪切板可能包含敏感信息，本项目应注意：

1. 默认只翻译英文文本；
2. 跳过疑似密码、Token、密钥内容；
3. 不保存完整剪切板历史；
4. 不上传非英文内容；
5. 不在日志中记录完整原文。

### 9.4.1 敏感内容过滤建议

以下内容应默认跳过：

```text
sk-
AKIA
BEGIN PRIVATE KEY
password=
token=
secret=
api_key=
```

---

## 10. 模块设计

## 10.1 剪切板监听模块

### 职责

* 监听 Windows 剪切板变化；
* 获取剪切板纯文本；
* 将文本发送给过滤模块。

### 输入

```text
Windows 剪切板更新事件
```

### 输出

```go
type ClipboardText struct {
    Text      string
    Timestamp time.Time
}
```

---

## 10.2 文本过滤模块

### 职责

* 判断文本是否需要翻译；
* 过滤无效、重复、敏感或非英文文本。

### 输入

```go
type ClipboardText struct {
    Text      string
    Timestamp time.Time
}
```

### 输出

```go
type FilterResult struct {
    ShouldTranslate bool
    Reason          string
    Text            string
}
```

### Reason 示例

```text
empty_text
duplicate_text
not_english
too_long
url
code
sensitive
ok
```

---

## 10.3 翻译模块

### 职责

* 调用 Eino；
* 构建 Prompt；
* 请求 LLM；
* 返回中文译文。

### 输入

```go
type TranslateRequest struct {
    SourceText string
    SourceLang string
    TargetLang string
}
```

### 输出

```go
type TranslateResponse struct {
    TranslatedText string
    Model          string
    DurationMs     int64
}
```

---

## 10.4 字幕推送模块

### 职责

* 将翻译结果推送给前端；
* 管理前端事件；
* 处理字幕覆盖。

### 事件名称

```text
translation:result
```

### 事件数据

```json
{
  "text": "这是翻译后的中文内容。",
  "source": "clipboard",
  "timestamp": 1710000000000
}
```

---

## 10.5 前端字幕模块

### 职责

* 接收翻译事件；
* 显示字幕；
* 执行动画；
* 控制自动隐藏。

### 状态

```ts
interface SubtitleState {
  text: string
  visible: boolean
  timer: number | null
}
```

---

## 11. 后端流程设计

```text
程序启动
  ↓
加载配置
  ↓
初始化日志
  ↓
初始化 LLM Client
  ↓
初始化 Eino Chain
  ↓
启动剪切板监听
  ↓
监听到剪切板变化
  ↓
读取剪切板文本
  ↓
文本过滤
  ↓
符合翻译条件
  ↓
调用 Eino 翻译
  ↓
获得中文译文
  ↓
推送给前端字幕窗口
```

---

## 12. 前端流程设计

```text
程序启动
  ↓
创建透明窗口
  ↓
窗口保持置顶
  ↓
监听后端 translation:result 事件
  ↓
收到译文
  ↓
更新字幕内容
  ↓
淡入显示
  ↓
停留指定时间
  ↓
淡出隐藏
```

---

## 13. 项目目录结构建议

```text
floating-translator/
├── README.md
├── config.example.yaml
├── go.mod
├── go.sum
├── main.go
├── internal/
│   ├── app/
│   │   └── app.go
│   ├── clipboard/
│   │   ├── listener.go
│   │   └── windows.go
│   ├── config/
│   │   └── config.go
│   ├── filter/
│   │   └── text_filter.go
│   ├── llm/
│   │   ├── translator.go
│   │   ├── eino_translator.go
│   │   └── prompt.go
│   ├── subtitle/
│   │   └── event.go
│   └── logger/
│       └── logger.go
├── frontend/
│   ├── package.json
│   ├── index.html
│   └── src/
│       ├── main.ts
│       ├── App.vue
│       ├── components/
│       │   └── SubtitleOverlay.vue
│       └── styles/
│           └── subtitle.css
├── build/
│   └── windows/
└── docs/
    ├── requirement.md
    ├── architecture.md
    └── development.md
```

---

## 14. 核心接口设计

## 14.1 翻译接口

```go
type Translator interface {
    Translate(ctx context.Context, text string) (string, error)
}
```

## 14.2 剪切板监听接口

```go
type ClipboardWatcher interface {
    Start(ctx context.Context, onChange func(text string)) error
    Stop() error
}
```

## 14.3 文本过滤接口

```go
type TextFilter interface {
    ShouldTranslate(text string) FilterResult
}
```

---

## 15. Eino 翻译链设计

## 15.1 Chain 结构

```text
Input Text
  ↓
Prompt Template
  ↓
ChatModel
  ↓
Output Parser
  ↓
Translated Text
```

## 15.2 Prompt Template

```text
你是一个专业的英文到中文翻译助手。

请将下面的英文内容翻译成自然、准确、流畅的简体中文。

要求：
- 只输出中文译文；
- 不要解释；
- 不要总结；
- 不要添加原文没有的信息；
- 保留必要的英文专有名词；
- 技术术语采用常见中文译法。

英文内容：
{{text}}
```

## 15.3 输出要求

LLM 最终只返回中文译文，例如：

```text
悬浮翻译器是一个监听 Windows 剪切板并自动显示翻译字幕的工具；当前 MVP 支持 Windows，产品后续可扩展其他平台。
```

不应返回：

```text
翻译如下：
悬浮翻译器是一个监听 Windows 剪切板并自动显示翻译字幕的工具；当前 MVP 支持 Windows，产品后续可扩展其他平台。
```

---

## 16. UI 设计

## 16.1 字幕布局

```text
┌──────────────────────────────────────────────┐
│                                              │
│                                              │
│                                              │
│                                              │
│                                              │
│                                              │
│             这是翻译后的中文字幕              │
│                                              │
└──────────────────────────────────────────────┘
```

## 16.2 字幕视觉风格

### 背景

```text
半透明黑色背景
圆角矩形
轻微模糊
```

### 字体

```text
白色字体
中等字重
较大字号
带轻微阴影
```

### 动画

```text
淡入
停留
淡出
```

---

## 17. 错误处理

## 17.1 剪切板读取失败

处理方式：

```text
记录日志
不中断程序
等待下一次剪切板更新
```

## 17.2 文本过滤失败

处理方式：

```text
跳过本次内容
记录 debug 日志
```

## 17.3 LLM 请求失败

处理方式：

```text
记录错误日志
不弹窗
不显示错误字幕
继续监听剪切板
```

## 17.4 前端事件推送失败

处理方式：

```text
记录日志
尝试重新推送或忽略
```

---

## 18. 日志设计

## 18.1 日志级别

```text
debug
info
warn
error
```

## 18.2 日志内容

建议记录：

```text
程序启动
配置加载成功
剪切板变化
文本过滤原因
翻译请求耗时
LLM 请求错误
字幕推送状态
程序退出
```

## 18.3 隐私保护

默认不记录完整剪切板原文。

可以记录：

```text
文本长度
文本 hash
过滤原因
请求耗时
模型名称
```

示例：

```text
INFO clipboard changed length=128 hash=abc123
INFO translate success duration=1532ms model=deepseek-chat
WARN skip text reason=url
ERROR translate failed error="request timeout"
```

---

## 19. 打包与运行

## 19.1 开发环境

```text
Windows 10 / Windows 11
Go 1.22+
Node.js 20+
Wails CLI
```

## 19.2 开发启动

```bash
wails dev
```

## 19.3 构建 Windows 应用

```bash
wails build
```

## 19.4 产物形式

```text
floating-translator.exe
config.yaml
logs/
```

---

## 20. MVP 版本范围

## 20.1 MVP 必须实现

* Windows 剪切板文本监听；
* 英文文本过滤；
* 重复内容过滤；
* Eino 调用 LLM 翻译；
* 翻译结果推送前端；
* 透明悬浮字幕显示；
* 自动淡入淡出；
* 基础配置文件；
* 基础日志。

## 20.2 MVP 暂不实现

* 用户登录；
* 翻译历史；
* 多语言自动识别；
* OCR 截屏翻译；
* 划词翻译；
* 系统热键；
* 复杂设置页面；
* 多模型切换 UI；
* 翻译结果手动编辑；
* 云端同步。

---

## 21. 后续可扩展功能

## 21.1 系统托盘控制

增加托盘菜单：

```text
暂停监听
恢复监听
打开配置
打开日志
退出程序
```

## 21.2 快捷键支持

例如：

```text
Ctrl + Alt + T：暂停/恢复翻译
Ctrl + Alt + C：重新翻译当前剪切板
Ctrl + Alt + H：隐藏字幕
```

## 21.3 翻译历史

可选保存最近翻译记录：

```text
原文
译文
时间
模型
```

默认不保存，保护隐私。

## 21.4 多模型支持

支持：

```text
OpenAI
DeepSeek
通义千问
火山方舟
Ollama
LM Studio
```

## 21.5 本地模型支持

可通过 Ollama 调用本地模型，减少隐私风险。

## 21.6 多屏幕支持

支持字幕显示在：

```text
主屏幕
鼠标所在屏幕
当前活动窗口所在屏幕
```

## 21.7 翻译模式扩展

支持：

```text
英文 → 中文
中文 → 英文
日文 → 中文
自动检测语言 → 中文
```

---

## 22. 关键实现难点

## 22.1 Windows 剪切板监听

需要正确处理 Windows 消息循环，监听剪切板更新事件，并避免因剪切板被其他程序占用导致读取失败。

## 22.2 鼠标穿透

透明窗口如果不做鼠标穿透，可能会挡住用户点击。正式版本建议实现 click-through。

## 22.3 前端透明窗口

需要处理 Wails 透明窗口、无边框窗口、始终置顶和背景透明之间的兼容问题。

## 22.4 LLM 请求频率控制

频繁复制文本时可能导致大量 LLM 请求，需要加入：

```text
防抖
重复过滤
并发控制
请求取消
```

## 22.5 隐私保护

剪切板内容可能包含密码、Token、邮箱、密钥等敏感信息，因此必须做敏感内容过滤，并避免日志记录完整原文。

---

## 23. 推荐默认参数

```yaml
clipboard:
  debounce_ms: 300
  max_text_length: 3000
  skip_duplicate: true
  skip_url: true
  skip_code: true
  only_translate_english: true

llm:
  temperature: 0.2
  timeout_seconds: 20

subtitle:
  width_percent: 70
  font_size: 28
  max_lines: 4
  background_opacity: 0.38
  fade_in_ms: 200
  display_ms: 6000
  fade_out_ms: 800
```

---

## 24. 验收标准

## 24.1 剪切板监听验收

* 复制英文句子后，程序能检测到剪切板变化；
* 复制同一段英文时，不重复翻译；
* 复制中文时，不触发翻译；
* 复制 URL 时，不触发翻译；
* 复制空白内容时，不触发翻译。

## 24.2 翻译功能验收

* 英文能够正确翻译为中文；
* 翻译结果不包含多余解释；
* 网络异常时程序不崩溃；
* LLM 请求超时时程序继续运行。

## 24.3 字幕显示验收

* 翻译结果能显示在屏幕中间偏下位置；
* 字幕背景为半透明；
* 字幕窗口无边框；
* 字幕显示后自动淡出；
* 新译文能覆盖旧译文；
* 字幕窗口不明显干扰用户操作。

## 24.4 稳定性验收

* 程序连续运行 2 小时无崩溃；
* 多次复制文本后无明显内存泄漏；
* LLM 请求失败不影响下一次翻译；
* 关闭程序后后台进程正常退出。

---

## 25. 开发阶段规划

## 25.1 第一阶段：基础原型

目标：

```text
实现剪切板监听 + 固定假翻译 + 前端字幕显示
```

任务：

* 搭建 Wails 项目；
* 创建透明字幕窗口；
* 实现前端字幕组件；
* 后端模拟翻译结果；
* 验证前后端事件通信。

## 25.2 第二阶段：接入剪切板

目标：

```text
真实监听 Windows 剪切板
```

任务：

* 实现 Windows 剪切板监听；
* 读取纯文本；
* 加入防抖；
* 加入重复过滤；
* 将剪切板文本推送到后端流程。

## 25.3 第三阶段：接入 Eino

目标：

```text
通过 Eino 调用 LLM 完成翻译
```

任务：

* 初始化 Eino ChatModel；
* 编写翻译 Prompt；
* 完成 Translator 接口；
* 配置模型参数；
* 处理请求失败和超时。

## 25.4 第四阶段：完善过滤与配置

目标：

```text
提高可用性和稳定性
```

任务：

* 增加英文判断；
* 增加 URL 过滤；
* 增加代码过滤；
* 增加敏感信息过滤；
* 增加 config.yaml；
* 增加日志模块。

## 25.5 第五阶段：打包测试

目标：

```text
形成可运行 Windows 桌面程序
```

任务：

* 使用 Wails 打包；
* 测试 Windows 10 / 11；
* 测试不同 DPI 和缩放比例；
* 测试多种复制场景；
* 修复透明窗口和置顶问题。

---

## 26. 最终产品形态

最终程序应表现为：

1.动程序；
2. 程序后台运行；
3. 用户复制英文文本；
4. 屏幕下方自动显示中文翻译；
5. 字幕数秒后自动消失；
6. 用户无需点击任何按钮；
7. 程序长期运行稳定；
8. 可通过配置文件调整模型和显示样式。

---

## 27. 简要总结

悬浮翻译器当前基于 Windows 剪切板监听、Golang 后端、Eino LLM 框架和透明悬浮前端窗口实现实时字幕翻译；产品命名不限定操作系统，后续可扩展其他平台。

其核心价值在于：

* 降低英文阅读时的翻译操作成本；
* ä¯»和工作流程；
* 以电影字幕形式自然展示译文；
* 基于 LLM 获得更自然的中文翻译；
* 使用 Go 技术栈便于后续扩展和打包部署。

MVP 阶段应重点保证：

```text
监听稳定
过滤准确
翻译可用
字幕美观
不打扰用户
隐私安全
```
