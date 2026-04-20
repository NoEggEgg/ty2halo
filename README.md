## Typecho 迁移到 Halo

用 Go 写的迁移工具，从 Typecho MySQL 数据库读取数据，通过 Halo OpenAPI 导入。

> **版本选择**
>
> 选错版本会浪费你的时间：
>
> | 版本                                              | 适合谁                    | 附件迁移  | 头图提取    |
> | ------------------------------------------------- | ------------------------- | --------- | ----------- |
> | [原项目](https://github.com/fghwett/typecho_to_halo) | 图片存在本地服务器        | ✅ 支持   | ❌ 不支持   |
> | **[本项目 (ty2halo)](https://github.com/NoEggEgg/ty2halo)** | 图片存在又拍云/七牛云/OSS | ❌ 已禁用 | ✅ 自动提取 |
>
> **一句话判断**：图片地址是 `https://你的域名/...` → 用原项目；是 `https://cdn.xxx.com/...` → 用本版本

## 能迁什么

- ✅ 标签（自动去重，避免重复创建）
- ✅ 多级分类（自动去重，保留层级结构）
- ❌ 附件（已禁用，远程图片保留原链接）
- ✅ 文章（自动提取第一张远程图片作为封面，自动去重）
- ✅ 页面（自动去重）
- ✅ 多级评论（自动过滤垃圾评论，保留层级关系）

## 使用前准备

### Halo 端

1. 生成 PAT 令牌：`个人中心` → `个人令牌` → `生成新令牌`
2. 清理数据：删除不需要的标签、分类、文章、页面、评论、附件
3. 关闭评论限制：`设置` → `评论设置` → 取消勾选 `仅允许注册用户评论`
4. 关闭邮件通知：`设置` → `通知设置` → 取消勾选 `启用邮件通知器`（防止导入评论时发邮件）
5. 安装编辑器：`应用市场` → 安装 `Vditor 编辑器` → `启用`
6. 配置头像：`插件` → `评论组件` → `头像设置` → 启用第三方头像，策略选 `匿名&无头像用户`
7. 创建备份：`备份` → `创建备份`（防万一）

### Typecho 端

1. 备份数据库
2. 准备好数据库连接信息

## 快速开始

### 1. 克隆仓库

```bash
git clone https://github.com/NoEggEgg/ty2halo.git
cd ty2halo
```

### 2. 配置

```bash
# 复制配置文件
cp config-example.yaml config.yaml

# 编辑 config.yaml
vim config.yaml
```

配置示例：

```yaml
typecho:
  type: mysql # 数据库类型 gorm支持范围
  baseUrl: https://your-domain.com/ # 必须以斜杠结尾
  dsn: {username}:{password}@tcp({ip}:{port})/{db_name}?charset=utf8mb4&parseTime=True&loc=Local # 修改数据库配置
  prefix: typecho_ # 数据库表前缀，默认为 typecho_

halo:
  host: domain:port # 域名（包含端口）
  schema: https # 协议
  token: {个人token} # Personal Access Token，权限要给足
  debug: false # 是否打印SDK日志
  policyName: default-policy # 默认存储策略
  groupName: "" # 文件分组名称，默认留空

fileManager:
  dir: ./_tmp/ # 文件缓存目录
```

### 3. 编译运行

```bash
# 编译
go mod tidy
go build -o typecho_to_halo.exe

# 运行
./typecho_to_halo.exe
```

或用 Task：

```bash
# 查看所有命令
task --list

# 直接运行迁移
task run
```

## 表前缀问题

如果你的 Typecho 表前缀不是 `typecho_`（比如是 `tc_`），需要在 MySQL 里创建视图：

```sql
CREATE OR REPLACE VIEW typecho_metas AS SELECT * FROM tc_metas;
CREATE OR REPLACE VIEW typecho_contents AS SELECT * FROM tc_contents;
CREATE OR REPLACE VIEW typecho_comments AS SELECT * FROM tc_comments;
CREATE OR REPLACE VIEW typecho_users AS SELECT * FROM tc_users;
CREATE OR REPLACE VIEW typecho_options AS SELECT * FROM tc_options;
CREATE OR REPLACE VIEW typecho_fields AS SELECT * FROM tc_fields;
CREATE OR REPLACE VIEW typecho_relationships AS SELECT * FROM tc_relationships;
```

详细说明见 [Typecho 迁移 Halo 完整教程：数据库视图解决表前缀 + 自动提取封面图](https://wuqishi.com/typecho-halo-migration-view-cover/)

## 开发环境要求

- [Git](https://git-scm.com/downloads)
- [Go 1.20+](https://golang.google.cn/dl)
- [Taskfile](https://taskfile.dev/installation)

### 初始化开发环境

```bash
# 下载 openapi 协议文件
task down-json

# 生成 openapi sdk
task gen-sdk

# 生成配置文件
task gen-config

# 生成数据库 sdk
task gen-db
```

## 代码修改说明

本版本主要修改：

1. **禁用附件迁移** - `apps/migrate/app.go` 中注释掉附件迁移 action
2. **添加头图提取** - 新增 `extractFirstImage` 函数，自动提取文章第一张远程图片作为封面
3. **数据去重检查** - 添加 `loadExistingData()` 函数，迁移前预加载 Halo 已有数据，通过 slug 对比避免重复创建标签、分类、文章和页面

## 常见问题

**Q: 迁移后标签/分类重复了？**
A: 工具已添加数据去重检查，会先查询 Halo 中已存在的数据（通过 slug 对比），避免重复导入。但如果手动删除 Halo 数据后重新迁移，仍然会正常导入。

**Q: 报错 `lookup xxx.comfiles: no such host`？**
A: `baseUrl` 必须以 `/` 结尾，比如 `https://example.com/`

**Q: 报错 `Table 'xxx.typecho_metas' doesn't exist`？**
A: 表前缀不匹配，用上面的数据库视图方案解决。

## 相关链接

- [详细迁移指南](https://wuqishi.com/typecho-halo-migration-view-cover/)
- [原项目 GitHub](https://github.com/fghwett/typecho_to_halo)
- [Halo 官方文档](https://docs.halo.run/)
- [Typecho 官网](https://typecho.org/)
