# sentinel 求解服务：常驻一个无头 Chromium 的 chatgpt.com 页面。
# 与主服务分开部署（独立容器/服务），主服务通过 SENTINEL_SOLVER_URL 调用它。
FROM node:22-bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    NODE_ENV=production \
    PORT=8080

WORKDIR /app

COPY package.json ./
RUN npm install --omit=dev --no-audit --no-fund

# channel="chromium" 需要完整 Chromium（不是 headless shell）
RUN npx playwright install --with-deps chromium
# Chromium 不支持带用户名密码的 SOCKS5 代理，用 gost 把这种上游转发成本机无认证的 SOCKS5
ARG GOST_VERSION=2.11.5
ARG TARGETARCH
RUN set -eux; \
    arch="${TARGETARCH:-$(dpkg --print-architecture)}"; \
    case "$arch" in amd64) ;; arm64) arch=armv8 ;; arm) arch=armv7 ;; *) echo "unsupported arch $arch"; exit 1 ;; esac; \
    apt-get update; \
    apt-get install -y --no-install-recommends curl ca-certificates; \
    rm -rf /var/lib/apt/lists/*; \
    name="gost-linux-${arch}-${GOST_VERSION}.gz"; \
    curl -fsSL --retry 3 --retry-delay 5 -o /tmp/gost.gz "https://github.com/ginuerzh/gost/releases/download/v${GOST_VERSION}/${name}" \
    || { \
      asset="$(curl -fsSL --retry 3 "https://api.github.com/repos/ginuerzh/gost/releases/tags/v${GOST_VERSION}" \
        | NAME="$name" node -e 'let d="";process.stdin.on("data",c=>d+=c).on("end",()=>{const a=JSON.parse(d).assets.find(x=>x.name===process.env.NAME);if(!a)process.exit(1);process.stdout.write(a.url)})')"; \
      curl -fsSL --retry 3 --retry-delay 5 -H "Accept: application/octet-stream" -o /tmp/gost.gz "$asset"; \
    }; \
    gunzip -c /tmp/gost.gz > /usr/local/bin/gost; \
    chmod +x /usr/local/bin/gost; \
    rm -f /tmp/gost.gz; \
    gost -V

COPY vm.js server.js ./

EXPOSE 8080

CMD ["node", "server.js"]
