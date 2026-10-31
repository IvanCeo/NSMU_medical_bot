FROM golang:1.27.1 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bot ./cmd/bot \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/backup ./cmd/backup \
    && mkdir -p /out/data && chown 65532:65532 /out/data

FROM gcr.io/distroless/static-debian13:nonroot
WORKDIR /app
COPY --from=build /out/bot /app/bot
COPY --from=build /out/backup /app/backup
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
ENTRYPOINT ["/app/bot"]
