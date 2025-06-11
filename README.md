# NTP-spy

Программа запускает NTP-сервер, который обрабатывает входящие запросы, генерирует NTP-ответы, получает геолокационные данные клиентов, а затем отправляет соответствующие уведомления админу в Telegram.

## Запуск с помощью Docker


```zsh
docker pull hermanguilliman/ntpspy:latest
docker run -d \
    --name NtpSpy \
    -e TELEGRAM_BOT_TOKEN=ваш_токен \
    -e TELEGRAM_ADMIN_ID=ваш_id \
    -e NTP_PORT=123 \
    hermanguilliman/ntpspy:latest
```

## Сборка и запуск из исходников с помощью compose

```zsh
git clone https://github.com/hermanguilliman/NtpSpy.git
cd NtpSpy
cp .env.example .env
nano .env
docker compose up -d --build
```