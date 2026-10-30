# The agent as one static binary with the dashboard embedded, on a distroless base: no shell, no package manager,
# runs as a non-root user. Build from the repo root: docker build -t consigliere .

FROM node:22.23.3-alpine3.24 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
# Writes the app into ../internal/web/dist, the folder the Go binary embeds.
RUN npm run build

FROM golang:1.27.2-alpine3.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web /src/internal/web/dist/ internal/web/dist/
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/consigliere ./cmd/consigliere

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
# The binary reads config/consigliere.json relative to the working directory; secrets and the data pack are mounted.
WORKDIR /app
COPY --from=build /out/consigliere /app/consigliere
COPY config/consigliere.json /app/config/consigliere.json
USER 65532:65532
ENTRYPOINT ["/app/consigliere"]
