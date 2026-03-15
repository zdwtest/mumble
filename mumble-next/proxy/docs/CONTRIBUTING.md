# 贡献指南

感谢您有兴趣为 Mumble WebSocket Proxy 做出贡献！

## 行为准则

请阅读并遵守我们的行为准则。我们致力于提供友好、安全和欢迎的环境。

## 如何贡献

### 报告 Bug

1. 搜索 [Issues](https://github.com/mumble/mumble/issues) 确认问题未被报告
2. 创建新 Issue，包含：
   - 清晰的标题和描述
   - 复现步骤
   - 预期行为和实际行为
   - 环境信息（操作系统、Go 版本等）
   - 相关日志

### 提交功能请求

1. 先开 Issue 讨论您的想法
2. 描述功能及其用例
3. 等待维护者反馈后再开始实现

### 提交代码

#### 开发环境设置

```bash
# 克隆仓库
git clone https://github.com/mumble/mumble.git
cd mumble/mumble-next/proxy

# 安装依赖
go mod download

# 安装开发工具
go install golangci-lint/cmd/golangci-lint@latest
```

#### 分支策略

- `master` - 稳定发布版本
- `develop` - 开发分支
- `feature/*` - 功能分支
- `fix/*` - Bug 修复分支

#### 提交流程

1. Fork 仓库
2. 创建功能分支
   ```bash
   git checkout -b feature/my-feature
   ```
3. 进行更改
4. 运行测试
   ```bash
   make test
   make lint
   ```
5. 提交更改
   ```bash
   git commit -m "feat: add new feature"
   ```
6. 推送到 Fork
   ```bash
   git push origin feature/my-feature
   ```
7. 创建 Pull Request

#### 提交信息格式

使用 [Conventional Commits](https://www.conventionalcommits.org/)：

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

**类型**：
- `feat`: 新功能
- `fix`: Bug 修复
- `docs`: 文档更改
- `style`: 代码格式（不影响功能）
- `refactor`: 代码重构
- `test`: 测试相关
- `chore`: 构建/工具相关

**示例**：
```
feat(auth): add OAuth2 support

Add support for OAuth2 authentication flow with
configurable providers (Google, GitHub, GitLab).

Closes #123
```

## 代码规范

### Go 代码风格

- 遵循 [Effective Go](https://golang.org/doc/effective_go)
- 使用 `gofmt` 格式化代码
- 使用 `goimports` 管理导入
- 运行 `golangci-lint` 检查

### 项目结构

```
proxy/
├── cmd/                    # 应用入口
│   └── mumble-proxy/
├── internal/               # 内部包
│   ├── admin/             # 管理 API
│   ├── auth/              # 认证模块
│   ├── config/            # 配置管理
│   ├── metrics/           # Prometheus 指标
│   ├── pool/              # 连接池
│   └── proxy/             # 代理核心
├── pkg/                    # 公共包
├── docs/                   # 文档
├── configs/                # 配置示例
└── test/                   # 测试
```

### 命名约定

- **包名**: 小写单词，如 `auth`, `metrics`
- **类型**: PascalCase，如 `TokenManager`
- **方法**: PascalCase，如 `ValidateToken`
- **常量**: PascalCase 或 UPPER_SNAKE_CASE
- **接口**: 动词+er，如 `Authenticator`, `ConnectionHandler`

### 注释规范

```go
// Package auth 提供 Token 认证功能。
//
// 支持多种认证方式：
//   - 静态 Token
//   - JWT Token
//   - OAuth2
package auth

// TokenManager 管理 Token 认证。
// 它是线程安全的，支持并发访问。
type TokenManager struct {
    // ...
}

// Validate 验证 Token 并返回对应的后端名称。
// 如果 Token 无效，返回空字符串和 false。
func (tm *TokenManager) Validate(token string) (backend string, ok bool) {
    // ...
}
```

### 错误处理

```go
// 使用 fmt.Errorf 包装错误
if err != nil {
    return fmt.Errorf("failed to connect: %w", err)
}

// 定义哨兵错误
var (
    ErrTokenInvalid = errors.New("token is invalid")
    ErrTokenExpired = errors.New("token has expired")
)

// 使用自定义错误类型
type ValidationError struct {
    Field   string
    Message string
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("%s: %s", e.Field, e.Message)
}
```

## 测试规范

### 单元测试

```go
func TestTokenManager_Validate(t *testing.T) {
    tm := NewTokenManager()
    tm.AddToken("test-backend", "test-token")

    tests := []struct {
        name      string
        token     string
        wantBackend string
        wantOK    bool
    }{
        {
            name:      "valid token",
            token:     "test-token",
            wantBackend: "test-backend",
            wantOK:    true,
        },
        {
            name:      "invalid token",
            token:     "invalid",
            wantBackend: "",
            wantOK:    false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            gotBackend, gotOK := tm.Validate(tt.token)
            if gotBackend != tt.wantBackend {
                t.Errorf("backend = %q, want %q", gotBackend, tt.wantBackend)
            }
            if gotOK != tt.wantOK {
                t.Errorf("ok = %v, want %v", gotOK, tt.wantOK)
            }
        })
    }
}
```

### 基准测试

```go
func BenchmarkTokenManager_Validate(b *testing.B) {
    tm := NewTokenManager()
    tm.AddToken("backend", "token")

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        tm.Validate("token")
    }
}
```

### 测试覆盖率

```bash
# 生成覆盖率报告
go test -coverprofile=coverage.out ./...

# 查看 HTML 报告
go tool cover -html=coverage.out

# 检查覆盖率阈值
# 目标: 所有包 >= 80%
```

## 文档贡献

### 文档位置

- `README.md` - 项目概述
- `docs/` - 详细文档
- 代码注释 - API 文档

### 文档风格

- 使用 Markdown 格式
- 包含代码示例
- 保持简洁明了
- 更新目录索引

## 发布流程

### 版本号规则

遵循 [语义化版本](https://semver.org/)：

- `MAJOR.MINOR.PATCH`
- `MAJOR`: 不兼容的 API 更改
- `MINOR`: 向后兼容的功能新增
- `PATCH`: 向后兼容的问题修复

### 发布步骤

1. 更新 `CHANGELOG.md`
2. 更新版本号
3. 创建 Git Tag
4. 构建 Release
5. 发布到 GitHub Releases

## 获取帮助

- **GitHub Issues**: 提问和报告问题
- **Discussions**: 一般讨论
- **文档**: `docs/` 目录

## 许可证

本项目采用 BSD 3-Clause 许可证。提交代码即表示您同意以相同许可授权。

---

再次感谢您的贡献！