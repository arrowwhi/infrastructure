# HTTP Server Handlers

## Использование

### 1. Health Check Handlers

```go
package main

import (
    "context"
    "errors"
    "github.com/arrowwhi/infrastructure/http/server"
    "github.com/arrowwhi/infrastructure/observability/logger"
)

// Пример кастомного health checker
type DBHealthChecker struct {
    db *sql.DB
}

func (c *DBHealthChecker) Check() error {
    return c.db.Ping()
}

func main() {
    logger.Init(&logger.Config{Level: "info"}, "my-service", "1.0.0", "production")
    
    cfg := server.DefaultConfig()
    srv := server.New(cfg, nil)
    
    // Регистрируем health handlers
    dbChecker := &DBHealthChecker{db: db}
    srv.RegisterHealthHandlers(dbChecker)
    
    // Устанавливаем сервер как готовый после инициализации
    srv.SetReady(true)
    
    // Запускаем сервер
    srv.Run(context.Background())
}
```

**Эндпоинты:**
- `GET /healthz` - всегда возвращает 200 OK
- `GET /ready` - возвращает 200 если сервер готов, 503 если нет

### 2. Swagger Documentation

Сначала добавьте аннотации в ваш код:

```go
package main

import (
    "github.com/arrowwhi/infrastructure/http/server"
    _ "your-project/docs" // Импортируем сгенерированную документацию
)

// @title Your API
// @version 1.0
// @description API документация
// @host localhost:8080
// @BasePath /api/v1
func main() {
    cfg := server.DefaultConfig()
    srv := server.New(cfg, nil)
    
    // Регистрируем Swagger UI
    srv.RegisterDocsHandler(server.DocsConfig{
        URL:  "/swagger/doc.json",
        Path: "/docs",
    })
    
    srv.Run(context.Background())
}
```

**Генерация документации:**
```bash
# Установить swag
go install github.com/swaggo/swag/cmd/swag@latest

# Сгенерировать документацию
swag init -g main.go -o ./docs
```

**Эндпоинты:**
- `GET /docs/index.html` - Swagger UI
- `GET /swagger/doc.json` - OpenAPI спецификация

### 3. Полный пример с HTTP3

```go
package main

import (
    "context"
    "crypto/tls"
    "crypto/x509"
    "github.com/arrowwhi/infrastructure/http/server"
    "github.com/arrowwhi/infrastructure/observability/logger"
)

func main() {
    logger.Init(&logger.Config{Level: "info"}, "my-service", "1.0.0", "production")
    
    // Загружаем TLS сертификаты
    serverCA, _ := tls.LoadX509KeyPair("server.crt", "server.key")
    clientCAPool := x509.NewCertPool()
    clientCAPool.AppendCertsFromPEM(rootCA)
    
    cfg := server.Config{
        Host:  "0.0.0.0",
        Port:  "8443",
        HTTP3: true,
        TLSConfig: &tls.Config{
            MinVersion:   tls.VersionTLS13,
            Certificates: []tls.Certificate{serverCA},
            ClientAuth:   tls.RequireAndVerifyClientCert,
            ClientCAs:    clientCAPool,
        },
    }
    
    srv := server.New(cfg, nil)
    
    // Регистрируем health handlers
    srv.RegisterHealthHandlers()
    
    // Регистрируем Swagger UI
    srv.RegisterDocsHandler(server.DocsConfig{Path: "/docs"})
    
    // Регистрируем свои хендлеры
    srv.Router().Get("/api/v1/users", getUsersHandler)
    srv.Router().Post("/api/v1/users", createUserHandler)
    
    // Устанавливаем готовность после инициализации
    srv.SetReady(true)
    
    // Запускаем сервер
    if err := srv.Run(context.Background()); err != nil {
        logger.Fatal(context.Background()).Err(err).Msg("Server failed")
    }
}
```

## Аннотации для Swagger

Пример хендлера с аннотациями:

```go
// @Summary Get user by ID
// @Description Получить пользователя по ID
// @Tags users
// @Accept json
// @Produce json
// @Param id path int true "User ID"
// @Success 200 {object} User
// @Failure 404 {object} ErrorResponse
// @Router /users/{id} [get]
func getUserHandler(w http.ResponseWriter, r *http.Request) {
    // ...
}
```

