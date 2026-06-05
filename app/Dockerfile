FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /app .

FROM scratch
COPY --from=build /app /app
EXPOSE 8080
ENTRYPOINT ["/app"]

HEALTHCHECK --interval=10s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/app", "-healthcheck"]
