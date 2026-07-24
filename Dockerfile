# --- build aşaması ---
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO_ENABLED=0: pgx saf Go, statik binary -> küçük runtime imajı
RUN CGO_ENABLED=0 go build -o /app/server ./src

# --- runtime aşaması ---
FROM alpine:3.20
# tzdata: APP_TIMEZONE için (yoksa dönem sessizce UTC'ye düşer)
# ca-certificates: Groq'a HTTPS çağrısı için (yoksa sertifika hatası)
RUN apk add --no-cache tzdata ca-certificates && adduser -D -H app
COPY --from=build /app/server /server
USER app
EXPOSE 8080
ENTRYPOINT ["/server"]