FROM golang:1.26.7-alpine@sha256:28d89ee9cc0ff9fec75c82ca201e6bf7fdf9a679d4b7b24dfa04f2bb766bb468 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY backend ./backend
COPY src/pkg/remote ./src/pkg/remote
COPY src/pkg/securestore ./src/pkg/securestore
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wrapper-api ./backend/cmd/api

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/wrapper-api /wrapper-api
USER nonroot:nonroot
ENTRYPOINT ["/wrapper-api"]
