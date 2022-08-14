FROM golang:alpine3.16 as builder

WORKDIR /app
COPY . .
RUN go build

# ---

FROM alpine:3.16

WORKDIR /app
COPY --from=builder /app/broadcast-server broadcast-server
COPY --from=builder /app/mainpage.html.tpl mainpage.html.tpl

ENTRYPOINT "/app/broadcast-server"
