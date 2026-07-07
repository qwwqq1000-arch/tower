# --- SPA build ---
# glibc (Debian) node, not alpine/musl: vite 8 / rolldown's native binding does
# not reliably install under `npm ci` on musl (npm optional-deps libc bug). This
# build stage is discarded in the multi-stage build, so the final runtime image
# below stays on alpine.
FROM node:26 AS spa
WORKDIR /spa
COPY web/spa/package.json web/spa/package-lock.json ./
RUN npm ci
COPY web/spa/ ./
RUN npm run build

# --- go build ---
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go env -w GOPROXY=https://goproxy.cn,direct && go mod download
COPY . .
COPY --from=spa /spa/dist ./web/spa/dist
RUN CGO_ENABLED=0 go build -o /out/tower ./cmd/tower

# --- run ---
FROM alpine:3.20
RUN adduser -D -u 10001 tower
COPY --from=build /out/tower /usr/local/bin/tower
USER tower
EXPOSE 8080
ENTRYPOINT ["tower"]
