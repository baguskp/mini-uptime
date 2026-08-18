FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod .
COPY main.go .
COPY web ./web
RUN go mod tidy
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/miniuptime .

FROM debian:12-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates iputils-ping \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/miniuptime /app/miniuptime
VOLUME ["/app/data"]
EXPOSE 3000
ENTRYPOINT ["/app/miniuptime"]
