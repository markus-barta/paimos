FROM golang:1.25-alpine AS go-build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /paimos .

FROM node:22-alpine AS spa-build
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --ignore-scripts
COPY frontend/ ./
# vite.config.ts reads ../VERSION relative to frontend/ (__dirname = /src here);
# AppChangelogModal.vue imports @docs/CHANGELOG.md?raw (alias -> ../docs/).
COPY VERSION /VERSION
COPY docs/ /docs/
RUN npm run build

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=go-build /paimos /usr/local/bin/paimos
COPY --from=spa-build /src/dist /app/static
RUN mkdir -p /app/data
VOLUME /app/data
ENV PORT=8888
ENV STATIC_DIR=/app/static
ENV DATA_DIR=/app/data
EXPOSE 8888
CMD ["paimos"]
