FROM golang:1.24-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

RUN mkdir -p /data

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/server /server
COPY --from=build --chown=nonroot:nonroot /data /data

USER nonroot
EXPOSE 8080
VOLUME ["/data"]

ENTRYPOINT ["/server"]
