// Package telegram — скелет Telegram-бота: всё, что повторяется в каждом боте, уже собрано,
// остаётся написать обработчики.
//
// Пакет состоит из четырёх частей (раньше это были отдельные пакеты telegram/app,
// telegram/bot, telegram/api и telegram/state; они остались как алиасы для обратной
// совместимости):
//
//   - запуск сервиса ([App], [NewApp], [AppConfig], [LoadConfig]): логгер, метрики, трейсинг,
//     health-проверки, служебный HTTP-порт и остановка в обратном порядке. Не привязан к
//     Telegram — им удобно запускать любой сервис;
//   - бот ([Bot], [New], [Config], [Context]): роутинг команд, кнопок и текста, шаги диалога,
//     воркеры с порядком внутри чата, polling и webhook, логи, метрики, трейсы и защита от паник;
//   - клиент Bot API ([Client], [NewClient], [ClientConfig]): лимиты Telegram (глобальный и на
//     чат), повтор после 429 с паузой из retry_after, токен не попадает в ошибки, метрики и
//     трейсы каждого вызова. Методы без обёртки вызываются через [Client.Call]. Клиент можно
//     использовать и без бота — например, для рассылок из воркера;
//   - хранилище шагов диалога ([Store], [State], [StateKey]): [NewMemoryStore] — в памяти
//     процесса (тесты, локальная разработка); [NewPostgresStore] — таблица PostgreSQL,
//     переживает рестарты и работает с несколькими копиями бота; [NewRedisStore] — ключи Redis
//     с TTL, устаревшие записи удаляет сам Redis.
//
// Типичный main:
//
//	var cfg struct {
//	    App      telegram.AppConfig
//	    Telegram telegram.Config
//	}
//	if err := telegram.LoadConfig(&cfg); err != nil { log.Fatal(err) }
//
//	a, err := telegram.NewApp(ctx, cfg.App)
//	if err != nil { log.Fatal(err) }
//
//	b, err := telegram.New(cfg.Telegram, telegram.WithStateStore(store))
//	if err != nil { log.Fatal(err) }
//
//	b.Command("start", "Начать", func(c *telegram.Context) error {
//	    _, err := c.Reply("Привет!")
//	    return err
//	})
//	b.Callback("rate", rateHandler)
//	b.OnState("awaiting_review", reviewHandler)
//	b.Text(searchHandler)
//
//	a.Health().Register("telegram", b.Healthcheck)
//	if err := a.Run(ctx, b); err != nil { log.Fatal(err) }
package telegram
