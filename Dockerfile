# Используем официальный образ Go
FROM golang:1.24-alpine

# Устанавливаем рабочую директорию
WORKDIR /app

# Копируем go.mod и go.sum для установки зависимостей
COPY go.mod go.sum ./

# Устанавливаем зависимости
RUN go mod download

# Копируем исходный код
COPY . .

# Компилируем приложение
RUN go build -o main .

# Команда для запуска приложения
CMD ["./main"]