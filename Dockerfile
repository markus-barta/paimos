FROM golang:1.25-alpine AS go-build
ARG SOURCE_DATE_EPOCH=0
ENV SOURCE_DATE_EPOCH=$SOURCE_DATE_EPOCH
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY VERSION /VERSION
RUN CGO_ENABLED=0 GOOS=linux go build \
  -trimpath \
  -buildvcs=false \
  -ldflags="-s -w -buildid= -X main.appVersion=$(tr -d ' \t\r\n' < /VERSION)" \
  -o /paimos . \
  && touch -d "@${SOURCE_DATE_EPOCH}" /paimos

FROM node:22-alpine AS spa-build
ARG SOURCE_DATE_EPOCH=0
ENV SOURCE_DATE_EPOCH=$SOURCE_DATE_EPOCH
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --ignore-scripts
COPY frontend/ ./
# vite.config.ts reads ../VERSION relative to frontend/ (__dirname = /src here);
# AppChangelogModal.vue imports @docs/CHANGELOG.md?raw (alias -> ../docs/).
COPY VERSION /VERSION
COPY docs/ /docs/
RUN npm run build \
  && find /src/dist -exec touch -d "@${SOURCE_DATE_EPOCH}" {} +

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
