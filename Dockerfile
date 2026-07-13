# 第一階段：用 Go 1.26 環境把執行檔編譯出來
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o registry ./cmd/registry

# 第二階段：換成健全的 Alpine 環境
FROM alpine:latest
# ✨ 關鍵：我們統一在 /app 目錄下活動
WORKDIR /app

# 把執行檔複製到 /app/registry
COPY --from=builder /app/registry .

# 把 data 資料夾複製到 /app/data，同時也在根目錄 /data 放一份（雙重保險！）
COPY data ./data
COPY data /data

# 在 Local 根目錄建立獨立的 `enterprise-allowlist.json` 進行版控
# 在這裡強行將它複製並蓋掉容器內部的 seed.json！
COPY enterprise-allowlist.json ./data/seed.json
COPY enterprise-allowlist.json /data/seed.json

EXPOSE 8080

# ✨ 終極核心：強制指定工作目錄在 /app，並且執行 ./registry
WORKDIR /app
ENTRYPOINT ["./registry"]