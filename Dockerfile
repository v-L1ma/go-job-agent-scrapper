FROM golang:1.26.3-bookworm AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -o /api ./cmd/main.go

RUN go run ./cmd/install-browsers/

FROM node:20-bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN npx playwright@1.52.0 install-deps chromium && \
    rm -rf /root/.npm /tmp/*

COPY --from=builder /root/.cache/ms-playwright /root/.cache/ms-playwright
COPY --from=builder /root/.cache/ms-playwright-go /root/.cache/ms-playwright-go
COPY --from=builder /api /api

EXPOSE 8080

CMD ["/api"]
