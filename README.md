# emby-reverse-proxy-go

Go 实现的 Emby 反向代理。通过路径编码上游协议、域名、端口和目标路径，将普通 HTTP 请求、媒体流和 WebSocket 请求转发到任意 Emby 服务；适合直接以 Docker 运行，并放在 Nginx Proxy Manager 或其他反向代理之后使用。

## 核心能力

- 通过 `/{scheme}/{domain}/{port}/{path}` 路径格式访问任意上游 Emby 服务
- 改写响应头中的绝对 URL：`Location`、`Content-Location`
- 对特定文本类型的未压缩响应体执行绝对 URL 改写
- 将代理 URL 中的 `Referer`、`Origin` 还原为真实上游 URL
- 清理代理特征请求头：`X-Real-Ip`、`X-Forwarded-*`、`Forwarded`、`Via`
- 清理部分响应特征头：`Server`、`X-Powered-By`、`X-Frame-Options`、`X-Content-Type-Options`
- 支持媒体流透传、Range 断点续传、WebSocket 透传

## URL 路径规则

唯一合法格式：

```text
/{scheme}/{domain}/{port}/{path}
```

规则如下：

- `scheme` 只能是 `http` 或 `https`
- `domain` 必填
- `port` 必填，范围必须是 `1-65535`，即使是 `80` 或 `443` 也不能省略
- `path` 可为空；为空时实际请求上游 `/`
- 查询参数会原样透传到上游
- 根路径 `/` 不是首页，直接访问会返回 `400 Bad Request`
- 固定健康检查路径只有 `/health`

示例：

```text
/https/emby.example.com/443/
/http/192.168.1.10/8096/web/index.html
/http/192.168.1.10/8096/emby/Items?api_key=xxxx
```

错误示例：

```text
/https/emby.example.com/web/index.html   # 缺少 port
/                                         # 根路径不是首页
```

## 快速开始

### docker run

```bash
docker run -d \
  --name emby-proxy-go \
  --restart unless-stopped \
  -p 8080:8080 \
  -e LISTEN_ADDR=:8080 \
  ghcr.io/gsy-allen/emby-proxy-go:v1.0
```

启动后：

- 服务默认监听容器内 `:8080`
- 宿主机暴露端口为 `8080`
- 健康检查地址：`http://127.0.0.1:8080/health`

### docker compose

```bash
docker compose up -d
```

等价配置如下：

```yaml
services:
  emby-proxy:
    image: ghcr.io/gsy-allen/emby-proxy-go:v1.0
    container_name: emby-proxy
    ports:
      - "8080:8080"
    environment:
      - LISTEN_ADDR=:8080
    restart: unless-stopped
```

## Nginx Proxy Manager 推荐配置

如果你将本项目放在 Nginx Proxy Manager 前面，推荐这样配置 Proxy Host：

- **Scheme**: `http`
- **Forward Hostname / IP**: docker网关地址，如`172.17.0.1`
- **Forward Port**: `8080`

推荐按钮状态：

- **WebSocket Support**: 开
- **Cache Assets**: 关
- **Block Common Exploits**: 建议先关，确认链路稳定后再自行评估是否开启

`Custom Nginx Configuration` 建议只填写下面三行：

```nginx
proxy_buffering off;
proxy_request_buffering off;
proxy_max_temp_file_size 0;
```

说明：

- 这三行用于减少中间层缓冲，避免大流量下载时发生额外预读和资源浪费
- 一般不要再手写 `proxy_set_header Connection ...` 或 `proxy_set_header Upgrade ...`，避免和 NPM 自己生成的配置冲突
- 如果你额外加入 `proxy_http_version 1.1;` 后出现网络错误，说明当前注入位置不适合覆写该指令，保留上面三行即可
- 一般不需要自己再包一层 `location / { ... }`
- 该程序会根据 `X-Forwarded-Proto`、`X-Forwarded-Host` 推断外部访问地址；如果前置代理没有正确传递这两个头，响应中改写后的 URL 可能不正确

## 使用示例

假设你的外部代理域名是 `https://proxy.example.com`：

| 场景 | 访问地址 |
|------|---------|
| Emby HTTPS 默认端口首页 | `https://proxy.example.com/https/emby.example.com/443/` |
| Emby HTTP 自定义端口首页 | `https://proxy.example.com/http/192.168.1.10/8096/` |
| API 请求 | `https://proxy.example.com/http/192.168.1.10/8096/emby/Items?api_key=xxxx` |
| Web 页面 | `https://proxy.example.com/http/192.168.1.10/8096/web/index.html` |

## 请求与响应改写行为

### 请求头处理

程序会转发原始请求头，但会主动清理以下代理相关头：

- `X-Real-Ip`
- `X-Forwarded-For`
- `X-Forwarded-Proto`
- `X-Forwarded-Host`
- `X-Forwarded-Port`
- `Forwarded`
- `Via`

另外：

- 如果请求里带有代理后的 `Referer`、`Origin`，程序会自动还原成真实上游 URL
- 发往上游时会将 `Host` 设置为目标主机和端口
- 非媒体请求会主动向上游发送 `Accept-Encoding: identity`，以便对文本响应进行 body 改写

### 响应头处理

程序会保留正常响应头，但会主动处理以下内容：

- 改写 `Location`
- 改写 `Content-Location`
- 移除 `Server`
- 移除 `X-Powered-By`
- 移除 `X-Frame-Options`
- 移除 `X-Content-Type-Options`

### 响应体改写条件

只有同时满足下面条件时，程序才会改写响应体中的绝对 URL：

1. `Content-Type` 属于以下类型之一：
   - `application/json`
   - `text/html`
   - `text/xml`
   - `text/plain`
   - `application/xml`
   - `application/xhtml`
   - `text/javascript`
   - `application/javascript`
2. `Content-Encoding` 为空，或者为 `identity`

注意：

- 如果上游返回的是 `gzip`、`br` 等压缩响应，程序不会先解压再改写，而是直接透传
- 若客户端声明支持 `gzip`，改写后的文本响应会重新以 `gzip` 返回
- 因此这不是“所有响应、所有类型、所有编码都会自动改写”

## 媒体流与 WebSocket

### 媒体流

媒体请求按路径和扩展名启发式识别，而不是依赖完整的 Emby API 语义。命中以下特征时通常会按流式透传处理：

- 路径包含 `/videos/`
- 路径包含 `/audio/`
- 路径包含 `/images/`
- 路径包含 `/items/images`
- 路径包含 `/stream`
- 文件扩展名属于视频、音频、图片、字体、压缩文件、字幕等已知类型

流式透传特性：

- 支持 `Range`
- 支持 `If-Range`
- 媒体类请求通常不会进入响应体改写路径

### WebSocket

WebSocket 透传依赖标准升级头：

- `Connection: Upgrade`
- `Upgrade: websocket`

如果前置代理没有正确透传升级头，WebSocket 无法建立。

## 健康检查

固定健康检查路径：

```text
/health
```

返回：

- HTTP 状态码：`200`
- 响应体：`ok`

注意：根路径 `/` 不是健康检查，也不是默认首页。

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `LISTEN_ADDR` | `:8080` | 服务监听地址 |

当前运行时只有这一个环境变量。

## 日志

```text
[API]    200 GET emby.example.com/emby/Items/123 (45ms)
[STREAM] 206 GET cdn.example.com/videos/xxx.mp4 | 1.2 GB | 3m22s
[PROXY]  200 GET emby.example.com/web/index.html | 156 KB | 89ms
[WS]     101 GET emby.example.com/socket | up 12 KB | down 48 KB | 2m10s
[ERROR]  GET cdn.example.com/videos/fail.mp4 : connection refused
```

含义：

- `[API]`：文本响应被读取并执行 body 改写
- `[STREAM]`：被识别为媒体流并按流式透传
- `[PROXY]`：普通透传请求，通常是非媒体二进制响应或未命中 body 改写条件的响应
- `[WS]`：WebSocket 连接，带上下行字节数和连接时长
- `[ERROR]`：上游连接失败或代理异常

## 限制与注意事项

- 根路径 `/` 不能作为首页使用，必须按 `/{scheme}/{domain}/{port}/{path}` 访问目标
- `port` 必填，默认端口也不能省略
- body 改写不是全覆盖，只对特定文本类型和未压缩/`identity` 响应生效
- 媒体识别是启发式规则，不保证对所有边界路径都分类完美
- 外部 URL 推断依赖 `X-Forwarded-Proto` 和 `X-Forwarded-Host`
- WebSocket 透传依赖标准 Upgrade 头完整到达本服务
- 程序会剥离部分响应安全头；如果你的部署依赖这些头，需要自行评估影响
- Docker 镜像本身不处理 TLS 终止；HTTPS 通常应由 Nginx Proxy Manager 或其他前置反代负责

## 验证步骤

### 1. 检查容器是否正常启动

```bash
curl http://127.0.0.1:8080/health
```

预期返回：

```text
ok
```

### 2. 验证根路径不是首页

```bash
curl -i http://127.0.0.1:8080/
```

预期返回 `400 Bad Request`。

### 3. 验证基础代理路径

将下面示例替换成真实 Emby 地址：

```bash
curl -i "http://127.0.0.1:8080/http/192.168.1.10/8096/"
```

### 4. 验证响应体 URL 改写

请求一个返回 JSON 或 HTML 的接口，检查其中绝对 URL 是否被改写为代理路径格式。

### 5. 验证媒体流与断点续传

对媒体资源发起带 `Range` 的请求，确认返回 `206 Partial Content`。

### 6. 验证 WebSocket

在前置代理开启 WebSocket Support 后，访问需要 WebSocket 的页面或接口；若失败，优先检查升级头是否被完整透传。

## 项目结构

```text
├── main.go              # 入口
├── handler.go           # HTTP 主流程编排与响应处理
├── headers.go           # 请求/响应头规则与辅助函数
├── target.go            # 代理路径解析与 target 语义
├── websocket.go         # WebSocket 透传逻辑
├── rewriter.go          # 响应改写：URL 替换、Header 改写
├── Dockerfile           # 多阶段构建镜像
└── docker-compose.yml   # 直接使用 GHCR 镜像的 Compose 示例
```
