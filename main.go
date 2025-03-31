package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

// Config - структура для конфигурации программы
type Config struct {
	NTPPort        string
	TelegramToken  string
	TelegramChatID string
}

// GeoIP - структура для хранения геолокационных данных
type GeoIP struct {
	Country string `json:"country"`
	City    string `json:"city"`
	ASN     string `json:"as"`
	ISP     string `json:"isp"`
}

var logger *zap.Logger

func init() {
	var err error
	logger, err = zap.NewProduction()
	if err != nil {
		fmt.Println("Ошибка инициализации логгера:", err)
		os.Exit(1)
	}
}

func main() {
	_ = godotenv.Load()

	config := Config{
		NTPPort:        os.Getenv("NTP_PORT"),
		TelegramToken:  os.Getenv("TELEGRAM_TOKEN"),
		TelegramChatID: os.Getenv("TELEGRAM_CHAT_ID"),
	}

	if config.NTPPort == "" || config.TelegramToken == "" || config.TelegramChatID == "" {
		logger.Fatal("Отсутствуют обязательные переменные окружения: NTP_PORT, TELEGRAM_TOKEN, TELEGRAM_CHAT_ID")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	msgChan := make(chan string, 100)
	go processTelegramMessages(ctx, config.TelegramToken, config.TelegramChatID, msgChan)

	go startNTPServer(ctx, config.NTPPort, msgChan)

	logger.Info("Программа запущена")

	<-sigChan
	logger.Info("Получен сигнал завершения, выполняем graceful shutdown")
	cancel()

	time.Sleep(2 * time.Second)
	logger.Info("Программа завершена")
}

// getGeoIP - функция для получения геолокационных данных по IP-адресу
func getGeoIP(ip string) (GeoIP, error) {
	var geo GeoIP
	url := fmt.Sprintf("http://ip-api.com/json/%s?fields=country,city,as,isp", ip)
	resp, err := http.Get(url)
	if err != nil {
		return geo, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return geo, fmt.Errorf("API вернул ошибку: %s", resp.Status)
	}

	err = json.NewDecoder(resp.Body).Decode(&geo)
	if err != nil {
		return geo, err
	}

	return geo, nil
}

// startNTPServer - запуск NTP-сервера с обработкой запросов и геолокацией
func startNTPServer(ctx context.Context, port string, msgChan chan<- string) {
	addr, err := net.ResolveUDPAddr("udp", ":"+port)
	if err != nil {
		logger.Fatal("Ошибка настройки адреса", zap.Error(err))
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		logger.Fatal("Ошибка запуска UDP сервера", zap.Error(err))
	}
	defer conn.Close()

	logger.Info("NTP сервер запущен", zap.String("port", port))

	bufPool := sync.Pool{
		New: func() interface{} { b := make([]byte, 48); return &b },
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("Остановка NTP сервера")
			return
		default:
			buf := *(bufPool.Get().(*[]byte))
			n, clientAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				logger.Warn("Ошибка чтения UDP", zap.Error(err))
				bufPool.Put(&buf)
				continue
			}

			if n == 48 {
				mode := buf[0] & 0x07
				if mode == 3 {
					response := makeNTPResponse()
					_, err = conn.WriteToUDP(response, clientAddr)
					if err != nil {
						logger.Warn("Ошибка отправки ответа клиенту", zap.Error(err))
					}

					// Извлекаем IP-адрес клиента
					ip := clientAddr.IP.String()

					// Получаем геолокационные данные
					geo, err := getGeoIP(ip)
					var msg string
					if err != nil {
						logger.Warn("Ошибка получения геолокационных данных", zap.Error(err))
						msg = fmt.Sprintf("Синхронизация NTP с клиентом: %s (геолокация не определена)", ip)
					} else {
						msg = fmt.Sprintf("Синхронизация NTP с клиентом: %s\nСтрана: %s\nГород: %s\nASN: %s\nПровайдер: %s",
							ip, geo.Country, geo.City, geo.ASN, geo.ISP)
					}

					// Отправляем сообщение в канал
					select {
					case msgChan <- msg:
					default:
						logger.Warn("Очередь сообщений переполнена, сообщение отброшено")
					}
				} else {
					logger.Info("Получен некорректный NTP-запрос",
						zap.String("client", clientAddr.String()),
						zap.Uint8("mode", mode))
				}
			} else {
				logger.Info("Получен пакет с некорректным размером",
					zap.String("client", clientAddr.String()),
					zap.Int("size", n))
			}

			bufPool.Put(&buf)
		}
	}
}

// makeNTPResponse - создание NTP-ответа
func makeNTPResponse() []byte {
	response := make([]byte, 48)
	response[0] = 0x1c

	now := time.Now().UTC()
	seconds := uint32(now.Unix() + 2208988800)
	fraction := uint32(now.Nanosecond() / 1000)

	response[40] = byte(seconds >> 24)
	response[41] = byte(seconds >> 16)
	response[42] = byte(seconds >> 8)
	response[43] = byte(seconds)
	response[44] = byte(fraction >> 24)
	response[45] = byte(fraction >> 16)
	response[46] = byte(fraction >> 8)
	response[47] = byte(fraction)

	return response
}

func processTelegramMessages(ctx context.Context, token, chatID string, msgChan <-chan string) {
	for {
		select {
		case <-ctx.Done():
			logger.Info("Остановка обработки сообщений Telegram")
			return
		case msg := <-msgChan:
			// Кодируем текст сообщения для URL
			text := url.QueryEscape(msg)
			urlStr := fmt.Sprintf(
				"https://api.telegram.org/bot%s/sendMessage?chat_id=%s&text=%s",
				token,
				chatID,
				text,
			)

			resp, err := http.Get(urlStr)
			if err != nil {
				logger.Warn("Ошибка отправки сообщения в Telegram",
					zap.Error(err),
					zap.String("url", urlStr),
				)
				continue
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				logger.Warn("Telegram API вернул ошибку",
					zap.String("status", resp.Status),
					zap.String("url", urlStr),
				)
			}
		}
	}
}
