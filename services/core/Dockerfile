FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/api ./cmd/api
COPY internal ./internal
COPY migrations ./migrations
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/aside-api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/aside-api /app/aside-api
COPY --from=build /src/migrations /app/migrations
ENV API_ADDR=0.0.0.0:8081 APP_ENV=production
EXPOSE 8081
USER 65532:65532
ENTRYPOINT ["/app/aside-api"]
