# 防汛水情监测与溃口抢险调度平台 —— 多阶段构建
# 同时支持 linux/amd64 与 linux/arm64。
FROM golang:1.22 AS build

ENV GOTOOLCHAIN=local \
    CGO_ENABLED=0

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .

RUN go build -trimpath -ldflags "-s -w" -o /out/floodctl ./cmd/floodctl

FROM gcr.io/distroless/static-debian12:nonroot AS runtime

WORKDIR /app

COPY --from=build /out/floodctl /usr/local/bin/floodctl

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/floodctl"]
CMD ["selfcheck"]
