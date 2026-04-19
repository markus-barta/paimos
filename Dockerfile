# Runtime-only image. Build artifacts first:
#   cd backend  && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ../dist/paimos .
#   cd frontend && npm ci && npm run build && cp -R dist ../dist/static
#   docker build -t paimos:local .
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY dist/paimos /usr/local/bin/paimos
COPY dist/static /app/static
RUN mkdir -p /app/data
VOLUME /app/data
ENV PORT=8888
ENV STATIC_DIR=/app/static
ENV DATA_DIR=/app/data
EXPOSE 8888
CMD ["paimos"]
