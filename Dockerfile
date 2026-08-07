# PetDB is a git submodule (see .gitmodules); the build context must
# already have it checked out — `git submodule update --init` before
# `docker build`, or clone with --recurse-submodules.

# --- frontend build -------------------------------------------------
FROM node:22-bookworm AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# --- backend build ----------------------------------------------------
FROM golang:1.26-bookworm AS backend
WORKDIR /src

COPY PetDB/ ./PetDB/
COPY server/ ./server/

# go:embed can only reach files inside the module tree, so the built
# frontend is copied into server/internal/webui/dist before compiling.
COPY --from=frontend /src/web/dist/ ./server/internal/webui/dist/

WORKDIR /src/server
RUN CGO_ENABLED=0 go build -o /out/jmessage ./cmd/jmessage

# --- runtime ------------------------------------------------------------
FROM gcr.io/distroless/static-debian12
COPY --from=backend /out/jmessage /usr/local/bin/jmessage
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/jmessage", "-addr", ":8080", "-data", "/data"]
