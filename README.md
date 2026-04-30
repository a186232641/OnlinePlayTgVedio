# OnlinePlayTgVedio

自部署 Web 应用,用于浏览、搜索、收藏、在线播放你已加入的 Telegram 频道里的视频。

底层基于 [gotd/td](https://github.com/gotd/td)(Go MTProto 客户端)。

## 功能

- 多用户注册系统(每个 Web 账号绑定自己的 TG 账号)
- Web UI 完成 TG 登录(手机号 → 验证码 → 二次验证密码)
- 自动索引你已加入的所有频道(包括超级群组)的视频消息,提取标题、时长、缩略图
- React SPA 浏览频道、视频网格、跨频道搜索(Postgres FTS)
- HTML5 `<video>` 通过 HTTP Range 流式播放,服务端按 4KB 对齐切块从 TG `upload.getFile` 拉取
- 收藏自动后台落盘缓存,非收藏视频按 LRU 淘汰
- `file_reference` 过期自动 lazy refresh

## 快速开始(本地开发)

依赖:Go 1.25+,Node 20+,Postgres 16+(或用 docker compose 起一个),Telegram API 凭证(从 https://my.telegram.org)

```bash
# 1. 配置环境
cp .env.example .env
# 编辑 .env,至少填入 TG_API_ID / TG_API_HASH
# 用下面命令生成 JWT_SECRET 和 MASTER_KEY:
openssl rand -base64 32   # JWT_SECRET
openssl rand -base64 32   # MASTER_KEY

# 2. 启动 Postgres(单独一个容器即可)
docker run -d --name tgv-pg \
  -e POSTGRES_USER=tgvideo -e POSTGRES_PASSWORD=change_me_in_prod -e POSTGRES_DB=tgvideo \
  -p 5432:5432 postgres:16-alpine

# 3. 启动后端(本地 :8080)
export $(grep -v '^#' .env | xargs)
export DB_DSN="postgres://tgvideo:change_me_in_prod@localhost:5432/tgvideo?sslmode=disable"
make run

# 4. 启动前端开发服务器(:5173,代理到 :8080)
cd web && npm install && npm run dev
```

打开 http://localhost:5173,注册账号 → 绑定 TG 账号 → 等待索引完成。

## 生产部署(docker compose)

```bash
cp .env.example .env
# 必填:TG_API_ID, TG_API_HASH, JWT_SECRET, MASTER_KEY,
# 改 POSTGRES_PASSWORD 和 DOMAIN(用于 Caddy)
docker compose -f deploy/docker-compose.yml --env-file .env --profile build up -d --build
```

服务:
- Caddy(80/443):反代 + auto-HTTPS + 静态托管 React 产物
- server(:8080):Go 后端
- postgres(:5432):索引 / 用户库
- volume `tgcache`:视频缓存(挂在 `/var/cache/tgvideo`)

## 关键架构决策

| 维度 | 实现 |
|---|---|
| 多用户 TG 客户端 | 每用户一个 `*telegram.Client`,~150KB 闲置开销;`bg.Connect` 维持连接;`floodwait` 中间件自动 backoff |
| TG 登录交互 | 三步 HTTP API + channel-driven `UserAuthenticator`:`/api/tg/login/{start,code,password}` |
| 会话加密 | TG session blob 用 AES-256-GCM 加密落 Postgres,密钥来自 env `MASTER_KEY` |
| Range 流式 | 浏览器 `Range: bytes=…` → 后端 4KB 对齐 + `tg.Client.UploadGetFile(Precise=true)` 1MiB 块循环;首块跳过对齐前缀字节,末块裁剪超出 |
| 缓存 | `cache_entries` 表 + 磁盘 `<CACHE_DIR>/videos/<doc_id>.bin`;收藏 = 后台 worker 整文件下载并 pin;LRU GC 每 5 分钟运行 |
| `file_reference` 刷新 | 流式中遇到 `FILE_REFERENCE_EXPIRED` → `channels.getMessages` 重取 → 更新 DB → 重试一次 |
| FTS | Postgres `tsvector` (simple 配置) on `videos.caption`;trigger 自动维护 |

## 环境变量

| 变量 | 必填 | 说明 |
|---|---|---|
| `TG_API_ID` | ✓ | https://my.telegram.org 申请 |
| `TG_API_HASH` | ✓ | 同上 |
| `DB_DSN` | ✓ | `postgres://user:pass@host:5432/db?sslmode=disable` |
| `JWT_SECRET` | ✓ | 任意字符串 |
| `MASTER_KEY` | ✓ | base64 编码的 32 字节随机密钥;**丢失则全员需重新绑定 TG** |
| `CACHE_DIR` | | 默认 `/var/cache/tgvideo` |
| `CACHE_CAP_GB` | | 默认 50 |
| `SERVER_ADDR` | | 默认 `:8080` |
| `DOMAIN` | | Caddy auto-HTTPS 用 |

## 仓库结构

```
cmd/server/                 入口
internal/
  config/                   env 解析
  db/                       pgx + 迁移 + 各表 repo
  auth/web/                 Web 注册/登录/JWT(argon2id)
  tgsession/                AES-GCM 加密的 gotd session.Storage 实现
  tgmanager/                每用户一个 *telegram.Client 生命周期
  tglogin/                  三步登录 channel 编排
  indexer/                  全量频道+视频扫描,缩略图下载
  video/                    Range stream 代理 + file_reference 刷新
  cache/                    favorites 后台下载 + LRU 淘汰
  api/                      chi router + handlers
web/                        Vite + React + TS + Tailwind SPA
deploy/                     Dockerfile / docker-compose.yml / Caddyfile
```

## 开发命令

```bash
make build       # go build -> bin/server
make run         # go run ./cmd/server
make test        # go test ./...
make vet         # go vet ./...
make tidy        # go mod tidy
make web-dev     # cd web && npm run dev
make compose-up  # docker compose up
```

## 注意事项与已知限制

- **MASTER_KEY 必须妥善保管**。建议用密钥管理服务(KMS)或在备份中加密存储 —— 丢失意味着所有用户存储的 TG 会话变砖。
- **首次索引可能数小时**。频道很多/历史很长时会触发 FLOOD_WAIT;`floodwait` 中间件会自动 sleep 然后重试。
- **缓存 dedup 按 `tg_doc_id`**(全局唯一)。同一公开频道里被多个用户收藏的同一视频只占一份磁盘空间。
- **不支持 CDN 文件**(`UploadFileCDNRedirect`)。极少数大文件可能走 CDN,这种情况下会返回 500;后续如有需要再增量支持 `getCdnFile` 流。
- **不支持 SignUp 流程**。已注册的 TG 账号可绑定;新号请先用官方客户端创建。

## License

MIT(待写 LICENSE 文件)
