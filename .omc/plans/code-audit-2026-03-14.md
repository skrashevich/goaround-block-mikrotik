# Аудит кода go-mikrotik-block

**Дата**: 2026-03-14
**Версия**: v0.0.2

---

## Критические ошибки

### 1. Игнорирование ошибки `getConfigFile()` (main.go:45, main.go:129)

```go
configFile, _ := getConfigFile() // ошибка отбрасывается
```

Если `os.UserConfigDir()` вернёт ошибку, `configFile` будет пустой строкой. Далее `k.Load("")` и `os.WriteFile("", ...)` приведут к непредсказуемому поведению.

**Исправление**: проверять ошибку и прерывать выполнение.

---

### 2. `updateRoutes()` всегда возвращает `nil` (main.go:215-225)

```go
func updateRoutes(...) error {
    if err := removeExistingRoutes(c, domain, dryRun); err != nil {
        fmt.Printf("Failed to remove existing routes: %v", err) // ошибка логируется, но не возвращается
    }
    for _, ip := range ips {
        if err := addRoute(...); err != nil {
            fmt.Printf("Failed to add route for IP %s: %v\n", ...) // то же
        }
    }
    return nil // всегда nil
}
```

Функция декларирует возврат `error`, но никогда не возвращает ошибок. Вызывающий код (`main.go:110`) проверяет ошибку, но она никогда не будет не-nil.

**Исправление**: аккумулировать ошибки через `errors.Join()` или возвращать первую ошибку.

---

### 3. `removeExistingRoutes` — отсутствие санитизации домена (main.go:228)

```go
r, err := c.Run("/ip/route/print", "?comment="+domain)
```

Домен передаётся без `sanitizeDomain()`, хотя в `addRoute()` санитизация выполняется. Потенциальная command injection через символ `=` в домене.

**Исправление**: использовать `sanitizeDomain(domain)`.

---

### 4. `parseFlags()` — флаг `version` не проверяется до валидации (main.go:160)

При вызове `-version` без остальных параметров программа выдаст ошибку "Missing required parameters" до того, как `main()` проверит флаг version (строка 81). Фактически `parseFlags` вернёт ошибку, и `os.Exit(1)` сработает раньше вывода версии.

**Исправление**: добавить `if version { return }` перед валидацией обязательных параметров.

---

## Серьёзные недоработки

### 5. Нет таймаута подключения к роутеру (main.go:208)

```go
return routeros.Dial(address, username, password)
```

`Dial` без таймаута — программа может зависнуть навечно при недоступном роутере.

**Исправление**: использовать `routeros.DialTimeout(address, username, password, 10*time.Second)` или аналог.

---

### 6. Нет TLS/шифрования соединения (main.go:190, 208)

Порт 8728 — plaintext API RouterOS. Логин/пароль передаются открытым текстом по сети.

**Исправление**: добавить поддержку порта 8729 (API-SSL) через `tls.Dial` или флаг `-tls`.

---

### 7. IPv6 маршруты добавляются с маской /32 (main.go:279)

```go
"=dst-address=" + ip.String() + "/32",
```

`net.LookupIP()` возвращает и IPv4, и IPv6 адреса. Для IPv6 маска `/32` некорректна (должна быть `/128`).

**Исправление**: проверять `ip.To4() != nil` и ставить `/32` для IPv4, `/128` для IPv6.

---

### 8. `listRoutesWithCommentAndGateway` — update в dry-run не работает (main.go:316)

```go
if update && !dryRun {
```

Логика: при `-update -dry` обновление пропускается полностью. Пользователь ожидает увидеть, что *было бы* обновлено, но ничего не происходит.

**Исправление**: `if update {` — функция `updateRoutes` внутри уже обрабатывает `dryRun`.

---

### 9. `resolveAndUpdateRoute` — ошибки игнорируются (main.go:317-319)

```go
resolveAndUpdateRoute(c, &filteredRoutes[i], route.Comment, dryRun)
```

Возвращаемое значение не проверяется (функция вообще `void`). Ошибки теряются молча.

**Исправление**: возвращать `error` из `resolveAndUpdateRoute`, обрабатывать в вызывающем коде.

---

### 10. Конфиг сохраняется с правами 0644 (main.go:131)

```go
return os.WriteFile(configFile, confBytes, 0644)
```

Конфиг может содержать чувствительные данные (address, username). Права 0644 позволяют чтение всем пользователям системы.

**Исправление**: `os.WriteFile(configFile, confBytes, 0600)`.

---

### 11. `MkdirAll` — ошибка игнорируется (main.go:39)

```go
os.MkdirAll(configPath, 0700)
```

Если создание директории не удалось, последующая запись конфига молча провалится.

**Исправление**: проверять ошибку.

---

## Слабые места архитектуры

### 12. Всё в одном файле `main.go` (362 строки)

Весь код в package `main` без разделения на пакеты. Затрудняет:
- Тестирование (невозможно подменить зависимости)
- Переиспользование
- Навигацию

**Рекомендация**: выделить пакеты `config`, `router`, `dns`.

---

### 13. Глобальное состояние `k` (koanf) (main.go:21)

```go
var k = koanf.New(".")
```

Глобальная переменная затрудняет тестирование и создаёт неявные зависимости.

**Рекомендация**: передавать конфиг как параметр или использовать struct.

---

### 14. `parseFlags()` возвращает 10 значений (main.go:134)

```go
func parseFlags() (domain, address, username, password, gateway string, listRoutes bool, doUpdate bool, dryRun bool, version bool, err error)
```

Нечитаемо. В тестах: `_, _, _, _, _, _, _, _, _, err := parseFlags()`.

**Рекомендация**: ввести `type Config struct { ... }` и возвращать `(*Config, error)`.

---

### 15. Тесты зависят от внешних сервисов (main_test.go:20)

```go
{"google.com", false}, // Assuming google.com will always resolve
```

Тест `TestResolveDomain` требует DNS и сеть. Будет падать в CI без сети или в air-gapped среде.

**Рекомендация**: использовать mock DNS resolver или пометить тест `//go:build integration`.

---

### 16. `TestSaveAndGetCreds` зависит от системного keyring (main_test.go:71)

Тест работает с реальным keyring ОС. В headless CI (Linux без D-Bus) упадёт.

**Рекомендация**: мокирование keyring или build tag.

---

## CI/CD проблемы

### 17. Устаревшие версии GitHub Actions (ci.yml)

| Action | Текущая | Актуальная |
|--------|---------|-----------|
| `actions/upload-artifact` | v3 | v4 |
| `actions/download-artifact` | v3 | v4 |
| `docker/setup-buildx-action` | v1 | v3 |
| `docker/login-action` | v1 | v3 |
| `docker/build-push-action` | v2 | v6 |
| `actions/create-release` | v1 | deprecated (использовать `softprops/action-gh-release`) |

v3 артефакты будут удалены GitHub — миграция на v4 обязательна.

---

### 18. Docker push на каждый push в любую ветку (ci.yml:4-6)

```yaml
on:
  push:
    branches:
      - '*'
```

Каждый push в любую ветку перезаписывает тег `:latest`. Должно быть ограничено `main` + тегами.

---

### 19. Нет тестов в CI (ci.yml)

Ни один workflow не запускает `go test`. Билд без тестов — бессмысленный CI.

**Исправление**: добавить шаг `go test ./...` перед Build.

---

### 20. Дублирование ci.yml и release.yml

Два workflow с практически одинаковым содержимым. DRY-нарушение.

**Рекомендация**: оставить один workflow с условным запуском.

---

## Dockerfile

### 21. `distroless/static-debian11` — устаревший образ

Debian 11 (Bullseye) — EOL июнь 2026. Следует перейти на `static-debian12`.

---

### 22. Версия Go не синхронизирована

- `go.mod`: `go 1.22`
- `Dockerfile`: `GO_VERSION="1.22.1"`
- `ci.yml`: `go-version: '1.22'`

При обновлении Go версия может разъехаться.

---

## Мелкие замечания

### 23. `fmt.Println(fmt.Errorf(...))` (main.go:59, 68)

Двойное форматирование ошибки. `fmt.Errorf` создаёт error, `Println` вызывает `.Error()` — лишняя обёртка. Проще: `fmt.Printf("error: %v\n", err)`.

---

### 24. Закомментированный код (main.go:294)

```go
// err := error(nil)
```

Мёртвый код — удалить.

---

### 25. `.gitignore` игнорирует `*_test.go`

```
*_test.go
```

Тестовые файлы НЕ должны быть в `.gitignore`. Это мешает другим разработчикам запускать тесты.

---

### 26. Сравнение ошибки по строке (main.go:67)

```go
if err != nil && err.Error() != "secret not found in keyring" {
```

Хрупкое — при изменении текста ошибки в библиотеке сломается. Лучше использовать `errors.Is()` или проверять тип.

---

### 27. Нет логгера (весь код)

Вся диагностика через `fmt.Printf`. Нет уровней логирования, нет возможности отключить вывод.

**Рекомендация**: `log/slog` (стандартная библиотека Go 1.21+).

---

## Приоритеты исправлений

| Приоритет | Пункты | Описание |
|-----------|--------|----------|
| **P0 — критично** | 1, 2, 3, 4 | Ошибки логики, потеря ошибок, уязвимость |
| **P1 — важно** | 5, 7, 8, 9, 10, 11, 17, 18, 19, 25 | Безопасность, корректность, CI |
| **P2 — улучшения** | 6, 12, 13, 14, 15, 16, 20, 21, 22, 27 | Архитектура, maintainability |
| **P3 — мелочи** | 23, 24, 26 | Стиль, чистка |
