# 项目自动化指令

当用户在对话中说 **“打包”** 或 **“build”** 时，请严格按照以下步骤执行命令。

---

## 打包流程

### 1️⃣ 执行构建命令
运行项目的构建脚本，生成最新的产物。
bash

wails build -clean

---

### 2️⃣ 复制构建产物
将构建生成的文件复制到指定的部署目录。
bash

cp -r build/bin/Git同步工具.exe E:/application/我的工具箱/Git同步工具.exe

复制
*(请确保目标目录存在，或者使用 `rsync` 替代)*

---

### 3️⃣ 更新 README.md
在 `README.md` 文件末尾追加最新的构建信息。

请执行以下逻辑（或写入文件）：
- 获取当前时间
- 获取当前 Git Commit Hash（短格式）

追加内容格式如下：
markdown

最新构建
构建时间: $(date)

Commit: $(git rev-parse --short HEAD)

状态: ✅ 已自动部署

复制
*(注意：如果 README.md 中已经存在 “## 最新构建” 标题，请先删除旧的那一段，再追加新的，避免重复)*

---

### 4️⃣ 提交并推送代码
将所有更改提交到 Git 仓库。
bash

git add .

git commit -m "chore: 自动打包构建 $(date)"

git push

复制
---

## 注意事项

1. 如果 `git push` 失败（例如没有 upstream），请提示用户手动处理。
2. 如果构建命令失败，立即停止后续步骤并报错。
3. 复制文件时，如果目标目录不存在，请尝试创建它（`mkdir -p`）。