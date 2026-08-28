# 项目编码强制规则（Kilo Code自动读取）
本项目存在公共包：`D:\code\github\wcj-go-common`，所有通用工具函数存放于此。

## 硬性编码规则，生成任何代码必须遵守：
1. 在生成业务代码前，先查阅本项目根目录 `common_api.md`。
2. 如果需要的工具能力 wcj-go-common 包已经实现：**禁止手写/重复实现工具逻辑，禁止复制wcj-go-common内部函数源码到业务代码**。
3. 直接 import 项目 wcj-go-common 包，调用已存在函数。
4. 只有 wcj-go-common.md 不存在的工具能力，才编写新的实现代码。
5. 当你不确定API具体行为的时候，调用读取工具读取 wcj-go-common 对应源码文件查看注释与实现。
6. 输出代码优先复用wcj-go-common，不要重复造轮子。
