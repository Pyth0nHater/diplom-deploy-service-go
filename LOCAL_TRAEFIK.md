# Local Traefik

Этот файл поднимает локальный `Traefik`, который будет видеть контейнеры, созданные вашим deploy-сервисом, и маршрутизировать их через один общий домен и path-based роуты.

## Запуск

```bash
docker compose -f docker-compose.traefik.yml up -d
```

После запуска будут доступны:

- `http://localhost:8081/dashboard/` - dashboard Traefik
- `http://localhost/<owner>/<repository>` - адрес развернутого приложения по умолчанию

## Как это связано с deploy-сервисом

В коде deploy-сервиса по умолчанию используется Docker network `web_network`:

- `DEPLOY_NETWORK=web_network`
- `DEPLOY_BASE_DOMAIN=localhost`
- `Host(\`<base-domain>\`) && (Path(\`/<owner>/<repository>\`) || PathPrefix(\`/<owner>/<repository>/\`))`

Именно поэтому `Traefik` в compose-файле тоже подключен к сети `web_network`.

## Как теперь строится адрес

Deploy-сервис использует один общий домен для всех приложений. По умолчанию это `localhost`, но его можно поменять через переменную окружения `DEPLOY_BASE_DOMAIN`.

Маршрут всегда строится из `repo_url`:

```text
http://<base-domain>/<owner>/<repository>
```

Пример:

```text
repo_url = https://github.com/acme/mai-wallet.git
-> http://localhost/acme/mai-wallet
```

## Пример сценария

1. Поднимите Traefik:

```bash
docker compose -f docker-compose.traefik.yml up -d
```

Если `8081` тоже занят, можно выбрать свой порт:

```bash
TRAEFIK_DASHBOARD_PORT=9090 docker compose -f docker-compose.traefik.yml up -d
```

2. Запустите ваш deploy-сервис:

```bash
GRPC_ADDR=:50051 go run ./cmd
```

3. Отправьте `DeployRequest` c `repo_url` и `image_name`.

4. Откройте в браузере:

```text
http://localhost/acme/mai-wallet
```

## Если домен не открывается

- Проверьте, что Traefik запущен: `docker ps`
- Проверьте dashboard: `http://localhost:8081/dashboard/`
- Проверьте, что контейнер приложения появился в сети `web_network`
- Проверьте, какой именно маршрут сервис вернул в событии `done`
- Убедитесь, что порт `80` не занят другим локальным сервером
