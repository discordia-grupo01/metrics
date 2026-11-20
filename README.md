# metrics

Servicio de métricas de Discordia.

## Tabla de contenidos

- [Introducción](#introducción)
- [Pre-requisitos](#pre-requisitos)
- [Cómo correr](#cómo-correr)
- [Variables de entorno](#variables-de-entorno)
- [Comandos Docker](#comandos-docker)
- [Tests](#tests)
- [Más documentación](#más-documentación)

## Introducción

`metrics` es el servicio que le muestra al staff cómo anda la plataforma. Se divide en tres partes:

- **Métricas de un servidor:** cuántos usuarios activos y mensajes tuvo en un período, la evolución diaria de sus miembros y un resumen que se puede exportar a CSV. Cuando un período no tuvo actividad, lo dice explícitamente.
- **Métricas globales:** la actividad de toda la plataforma.
- **Estado de la plataforma:** consulta cada tanto el estado de los demás servicios, y permite al staff publicar y resolver incidentes.

Está hecho en Go con Gin y guarda todo en su propia base PostgreSQL. No pregunta nada a otros servicios: arma su propia proyección de membresía y mensajes a partir de los eventos de RabbitMQ, y las consultas se hacen sobre esa copia.

Todas las métricas son solo para el staff. Quién es staff lo sabe por los eventos firmados (HMAC) que publica `admin`, y el gateway (Kong) valida el JWT antes de llegar acá.

La API REST está descripta en [`openapi.yaml`](openapi.yaml).

## Pre-requisitos

- **Docker** y **Docker Compose** v2.24 o más nuevo.
- **RabbitMQ**, solo si querés que el servicio reciba eventos y las métricas se llenen. Se levanta con el repositorio [`rabbit-mq`](https://github.com/discordia-grupo01/rabbit-mq). Sin él, la API arranca igual pero los números quedan en cero.
- **Go 1.22** y **make**, solo si querés correr el servicio con Go en tu máquina.

## Cómo correr

### Con Docker Compose

Levanta el servicio y su base PostgreSQL, sin configurar nada:

```bash
docker compose up --build
```

Antes de arrancar el servicio, un contenedor `migrate` crea las tablas y termina. Cuando todo esté arriba, comprobá que el servicio esté vivo:

```bash
curl http://localhost:8080/readyz
```

Para probarlo sin JWT, mandá el header `X-User-Id` con el usuario de staff de prueba (en el compose ya está activado):

```bash
curl http://localhost:8080/v1/metrics/global \
  -H "X-User-Id: 11111111-1111-4111-8111-111111111111"
```

Si querés cambiar puertos, usuario de la base o los secretos de prueba, copiá el archivo de ejemplo y editalo antes de levantar:

```bash
cp .env.example .env
```

Este compose levanta solo lo que es de este servicio. Para el sistema completo (con el gateway y los demás servicios) usá el compose del repositorio `infrastructure`.

### Con eventos (RabbitMQ)

Para que las métricas se llenen, levantá primero RabbitMQ desde el repositorio `rabbit-mq`:

```bash
cd ../rabbit-mq
docker compose up -d
```

Después, en el `.env` de este repositorio, completá la variable según cómo corras el servicio:

| Cómo lo corrés | Qué completar en `.env` |
|---|---|
| Con `docker compose up` | `RABBITMQ_URL_DOCKER=amqp://guest:guest@host.docker.internal:5672/` |
| Con `make run` | `RABBITMQ_URL=amqp://guest:guest@localhost:5672/` |

Desde un contenedor, `localhost` es el propio contenedor, por eso hace falta `host.docker.internal`.

### Para desarrollar

Si vas a tocar el código, te conviene correr el servicio con Go en tu máquina y dejar en un contenedor solo la base:

```bash
cp .env.example .env
docker compose up -d db migrate
make run
```

Para crear las tablas, `docker compose up -d` también corre el contenedor `migrate`, que aplica las migraciones de la carpeta `migrations/` y termina. Si usás otra base, `make migrate` hace lo mismo con la herramienta [golang-migrate](https://github.com/golang-migrate/migrate) instalada en tu máquina, tomando la conexión de `DATABASE_URL`.

## Variables de entorno

Se leen del entorno o de un archivo `.env`. El archivo [`.env.example`](.env.example) trae valores listos para correr en local.

| Variable | Default | Descripción |
|---|---|---|
| `ENVIRONMENT` | `development` | `development` o `production`. En producción Gin usa release mode. |
| `HOST` | `0.0.0.0` | IP donde escucha el servicio. |
| `PORT` | `8080` | Puerto donde escucha el servicio. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warning` o `error`. |
| `DATABASE_HOST` | `localhost` | Host de PostgreSQL. |
| `DATABASE_PORT` | `5432` | Puerto de PostgreSQL. |
| `DATABASE_NAME` | `discordia_metrics` | Nombre de la base. |
| `DATABASE_USER` | `discordia` | Usuario de la base. |
| `DATABASE_PASSWORD` | `discordia` | Contraseña de la base. En producción es obligatoria y secreta. |
| `DATABASE_SSLMODE` | `disable` | `disable` en local, `require` en producción. |
| `DATABASE_URL` | vacío | Cadena de conexión completa. Si está, tiene prioridad sobre las variables `DATABASE_*`. |
| `RABBITMQ_URL` | vacío | Conexión a RabbitMQ. Vacía = la API arranca pero no consume eventos. |
| `EVENTS_EXCHANGE` | `discordia.events` | Exchange compartido por los servicios. |
| `EVENTS_QUEUE` | `metrics` | Cola de este servicio. |
| `STAFF_USER_IDS` | vacío | Ids de staff de arranque, separados por coma. La fuente principal son los eventos de `admin`. |
| `ADMIN_EVENTS_HMAC_SECRET` | vacío | Secreto compartido con `admin` para validar los cambios de staff. Es un secreto. |
| `USER_ID_HEADER` | vacío | Con `X-User-Id`, el servicio toma el usuario de ese header en vez del JWT. Solo para desarrollo: nunca en un entorno desplegado. |
| `HEALTH_CHECKS` | vacío | Lista de `servicio=URL` que se consultan para mostrar el estado de la plataforma. Son las rutas públicas del gateway (`/health/...`). |
| `HEALTH_CHECK_INTERVAL` | `30s` | Cada cuánto se consulta cada servicio. |
| `HEALTH_CHECK_TIMEOUT` | `3s` | Tiempo máximo de cada consulta. |
| `HEALTH_FAILURE_THRESHOLD` | `2` | Fallos seguidos para marcar un servicio como degradado. |
| `HEALTH_RECOVERY_THRESHOLD` | `2` | Respuestas buenas seguidas para recuperarlo. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | vacío | Backend OpenTelemetry (por ejemplo New Relic). Vacío = sin trazas ni métricas. |
| `OTEL_EXPORTER_OTLP_HEADERS` | vacío | Credenciales del backend. Es un secreto. |
| `LOG_SHIP_URL` | vacío | Si se completa, también manda los logs a New Relic. |

Variables que usa solo `docker-compose.yml`:

| Variable | Default | Descripción |
|---|---|---|
| `API_PORT` | `8080` | Puerto del servicio en tu máquina. |
| `DATABASE_PORT` | `5432` | Puerto de PostgreSQL en tu máquina. |
| `RABBITMQ_URL_DOCKER` | vacío | Conexión a RabbitMQ para el servicio que corre dentro del compose. |

## Comandos Docker

Todos desde la raíz del repositorio.

| Qué querés hacer | Comando |
|---|---|
| Levantar todo | `docker compose up --build` |
| Levantar todo en segundo plano | `docker compose up -d --build` |
| Levantar solo la base | `docker compose up -d db` |
| Ver los logs del servicio | `docker compose logs -f api` |
| Ver qué está corriendo | `docker compose ps` |
| Reconstruir solo el servicio | `docker compose up -d --build api` |
| Abrir una consola de la base | `docker compose exec db psql -U discordia -d discordia_metrics` |
| Bajar todo, conservando los datos | `docker compose down` |
| Bajar todo y borrar los datos | `docker compose down -v` |

Las migraciones nuevas se aplican solas cada vez que levantás el compose.

Para construir y correr la imagen a mano, contra una base que ya tengas:

```bash
docker build -t metrics .
docker run --rm -p 8080:8080 --env-file .env metrics
```

Dentro de un contenedor, `DATABASE_HOST` y `RABBITMQ_URL` no pueden apuntar a `localhost`: tienen que usar el nombre del contenedor o `host.docker.internal`.

## Tests

```bash
make test
```

El CI exige una cobertura mínima del 70 % y además corre `gofmt`, `go vet`, el build de la imagen y la validación del `openapi.yaml` (`make openapi`) en cada pull request. También hay mutation testing de la lógica de métricas y de estado (`make mutation`, tarda menos de un minuto y no corre en el CI).

El repositorio se formatea con gofmt. Activá el hook una vez por clon:

```bash
git config core.hooksPath .githooks
```

### Tests de contrato

`openapi.yaml` se genera desde las anotaciones swag del código (`make openapi`) y
se valida con [Schemathesis](https://schemathesis.readthedocs.io): genera casos a
partir del contrato, los manda al servicio real y verifica que las respuestas
(códigos, esquemas, content-type, cabeceras) coincidan con lo declarado.

```bash
make contract-deps-up   # Postgres (55502) y RabbitMQ (55512) efímeros en Docker, con migraciones
make contract-test      # levanta el servicio en 18082 y corre Schemathesis
make contract-deps-down
```

Hace falta Python 3 (instala Schemathesis 4.33.4 en `.contract-venv/`), Go y
Docker. El script solo corre contra servicios locales y rechaza cualquier otro
host; usa un staff de prueba y secretos de juguete, y deja `HEALTH_CHECKS`
vacío para no hacer llamadas de red. `CONTRACT_MAX_EXAMPLES` ajusta los casos por
operación (25 por defecto). Además de Schemathesis, el script comprueba las
ramas 401 y 403 con un usuario que no es staff. Quedan desactivados tres checks,
con el motivo en `scripts/contract-test.sh`: `positive_data_acceptance`,
`negative_data_rejection` y `unsupported_method`. En el CI corre el job
`contract`, en paralelo a `test` y sin `secrets`.

El script hace dos corridas. La primera usa ids y fechas al azar. La segunda
siembra eventos de prueba (`scripts/contract/seed.sql`) y fija el servidor y el
período de esos datos con un hook (`scripts/contract/hooks.py`), para validar
contra el contrato respuestas con contadores distintos de cero. Al final
`scripts/contract/coverage.py` cuenta, con el HAR de Schemathesis, cuántas
operaciones obtuvieron al menos un 2xx real, y el script falla si alguna no.

## Más documentación

En este repositorio:

- [`docs/historias-de-usuario.md`](docs/historias-de-usuario.md): las historias del catálogo que resuelve este servicio, con sus rutas.
- [`openapi.yaml`](openapi.yaml): contrato de la API REST.
- [`docs/mutation-testing`](docs/mutation-testing/README.md): resultados del mutation testing.

En el repositorio [`docs`](https://github.com/discordia-grupo01/docs):

- [`asyncapi`](https://github.com/discordia-grupo01/docs/tree/main/asyncapi): los eventos que este servicio consume y publica.
