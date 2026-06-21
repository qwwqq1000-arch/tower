# Tower — Claude 号池调度总控台

Go 单二进制 + Postgres。把多台 new-meridian 哑节点统一调度:封号检测/半开恢复、并发槽冷却、三层封控、保底中转、计费、审计。new-api 只需指向 Tower 的一个 dispatchKey。

## 快速搭建

```bash
git clone <repo> tower && cd tower
cp .env.example .env
# 生成密钥并填进 .env:
echo "TOWER_MASTER_KEY=$(openssl rand -base64 32)"
echo "TOWER_SESSION_SECRET=$(openssl rand -hex 32)"
# 编辑 .env 设好 POSTGRES_PASSWORD / TOWER_ADMIN_PASSWORD
docker compose up -d --build
```

打开 `http://<服务器>:8080` → 用 `.env` 里的 `TOWER_ADMIN_USER/PASSWORD` 登录。

## 接入

1. **加节点**:控制台「节点」→ 填 new-meridian 地址(`http://ip:3456`)+ 其 api key。
2. **建调度密钥**:控制台「调度密钥」→ 新建 → 复制明文(仅显示一次)。
3. **配 new-api**:渠道地址填 `http://<tower>:8080`,密钥填上一步的 `dk_...`,模型走 `/v1/messages`。

## 环境变量

| 变量 | 说明 |
|---|---|
| `TOWER_DATABASE_URL` | Postgres 连接串(compose 自动注入) |
| `TOWER_MASTER_KEY` | 号库凭据 AES-256 主密钥,base64 的 32 字节 |
| `TOWER_SESSION_SECRET` | 会话 cookie 签名密钥 |
| `TOWER_ADMIN_USER/PASSWORD` | 空库时自动建的首个 superadmin |
| `TOWER_HTTP_ADDR` | 监听地址,默认 `:8080` |

## 升级

```bash
git pull && docker compose up -d --build
```
迁移在启动时自动应用(goose,内嵌)。

## 状态

后端 + 网关已就绪(`POST /v1/messages` 可用)。流式代理、完整 React 控制台、节点一键开通、遥测轮询为后续增强。
