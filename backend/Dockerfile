FROM golang:1.24-alpine AS build
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /projectview-server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /projectview-server ./projectview-server
ENV NODE_ENV=production
EXPOSE 4000
CMD ["./projectview-server"]
