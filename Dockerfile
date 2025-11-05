# --- build ---
FROM golang:1.22 AS build
WORKDIR /app
COPY go.mod .
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

# --- runtime ---
FROM gcr.io/distroless/base-debian12
WORKDIR /
COPY --from=build /bin/server /server
EXPOSE 8080
ENV HTTP_PORT=8080
CMD ["/server"]
