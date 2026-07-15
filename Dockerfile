# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# proxy.golang.org is often unreachable from mainland-China ECS instances.
# Use Alibaba Cloud's Go module mirror for the image build, then fall back to
# direct module downloads only when the mirror reports a module as missing.
ENV GOPROXY=https://mirrors.aliyun.com/goproxy/,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata
ENV TZ=Asia/Shanghai

WORKDIR /app

COPY --from=builder /server ./server
COPY conf/ ./conf/

EXPOSE 9090 8080

CMD ["./server", "-config", "conf/server.yaml"]
